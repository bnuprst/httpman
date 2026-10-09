// Package httpclient sends fully-resolved Postman requests.
package httpclient

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptrace"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/andybalholm/brotli"

	"github.com/bnuprst/httpman/internal/collection"
)

// Version is reported in the default User-Agent.
var Version = "dev"

// Options control how requests are sent.
type Options struct {
	Timeout            time.Duration
	FollowRedirects    bool
	MaxRedirects       int
	InsecureSkipVerify bool
	ProxyMode          string // "system" (default), "none", "custom"
	ProxyURL           string
	MaxResponseBytes   int64
	BaseDir            string // base for relative file paths in bodies
	Jar                http.CookieJar
	DisableCookies     bool
}

// DefaultOptions returns sensible defaults.
func DefaultOptions() Options {
	return Options{Timeout: 0, FollowRedirects: true, MaxRedirects: 10, MaxResponseBytes: 100 << 20}
}

// Request is a resolved request (no {{variables}} left).
type Request struct {
	Method string             `json:"method"`
	URL    string             `json:"url"`
	Header collection.Headers `json:"header"`
	Body   *collection.Body   `json:"body,omitempty"`
	Auth   *collection.Auth   `json:"auth,omitempty"`
}

// Timings in milliseconds.
type Timings struct {
	DNS       float64 `json:"dns"`
	Connect   float64 `json:"connect"`
	TLS       float64 `json:"tls"`
	FirstByte float64 `json:"firstByte"`
	Download  float64 `json:"download"`
	Total     float64 `json:"total"`
}

// SentRequest records what actually went over the wire.
type SentRequest struct {
	Method string             `json:"method"`
	URL    string             `json:"url"`
	Header collection.Headers `json:"header"`
	Body   string             `json:"body,omitempty"`
}

// Response is the result of sending a request.
type Response struct {
	Code       int                `json:"code"`
	Status     string             `json:"status"`
	Proto      string             `json:"proto"`
	Header     collection.Headers `json:"header"`
	Body       []byte             `json:"-"`
	Time       float64            `json:"responseTime"`
	Timings    Timings            `json:"timings"`
	BodySize   int64              `json:"bodySize"`
	HeaderSize int64              `json:"headerSize"`
	Truncated  bool               `json:"truncated,omitempty"`
	Request    SentRequest        `json:"request"`
	Redirects  []string           `json:"redirects,omitempty"`
	RemoteAddr string             `json:"remoteAddr,omitempty"`
	Warnings   []string           `json:"warnings,omitempty"`
}

// Client sends requests, reusing connections between calls.
type Client struct {
	opts      Options
	transport *http.Transport
}

// New creates a client.
func New(opts Options) *Client {
	t := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   30 * time.Second,
		ExpectContinueTimeout: time.Second,
		DisableCompression:    true,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: opts.InsecureSkipVerify}, //nolint:gosec // user-controlled setting
	}
	switch opts.ProxyMode {
	case "none":
		t.Proxy = nil
	case "custom":
		if u, err := url.Parse(opts.ProxyURL); err == nil && opts.ProxyURL != "" {
			t.Proxy = http.ProxyURL(u)
		}
	}
	if opts.MaxRedirects == 0 {
		opts.MaxRedirects = 10
	}
	if opts.MaxResponseBytes == 0 {
		opts.MaxResponseBytes = 100 << 20
	}
	return &Client{opts: opts, transport: t}
}

// Close releases idle connections.
func (c *Client) Close() { c.transport.CloseIdleConnections() }

// Options returns the client options.
func (c *Client) Options() Options { return c.opts }

// RequestOverrides are per-request settings (protocolProfileBehavior).
type RequestOverrides struct {
	FollowRedirects         *bool
	MaxRedirects            *int
	StrictSSL               *bool
	FollowOriginalMethod    *bool
	RemoveRefererOnRedirect *bool
}

func isRedirect(code int) bool {
	switch code {
	case 301, 302, 303, 307, 308:
		return true
	}
	return false
}

// Do sends the request.
func (c *Client) Do(ctx context.Context, r *Request, ov RequestOverrides) (*Response, error) {
	u, err := ParseURL(r.URL)
	if err != nil {
		return nil, err
	}
	body, contentType, err := buildBody(r.Body, c.opts.BaseDir)
	if err != nil {
		return nil, err
	}
	header := http.Header{}
	host := ""
	for _, h := range r.Header {
		if h.Disabled || strings.TrimSpace(h.Key) == "" {
			continue
		}
		k := textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(h.Key))
		switch k {
		case "Host":
			host = h.Value
			continue
		case "Content-Length":
			continue
		}
		header.Add(k, h.Value)
	}
	if contentType != "" && header.Get("Content-Type") == "" {
		header.Set("Content-Type", contentType)
	}
	if header.Get("User-Agent") == "" {
		header.Set("User-Agent", "httpman/"+Version)
	}
	if header.Get("Accept") == "" {
		header.Set("Accept", "*/*")
	}
	if header.Get("Accept-Encoding") == "" {
		header.Set("Accept-Encoding", "gzip, deflate, br")
	}
	if header.Get("Connection") == "" {
		header.Set("Connection", "keep-alive")
	}

	method := strings.ToUpper(strings.TrimSpace(r.Method))
	if method == "" {
		method = "GET"
	}
	var warnings []string
	authState, err := applyAuth(r.Auth, method, u, header, body, &warnings)
	if err != nil {
		return nil, err
	}

	transport := c.transport
	if ov.StrictSSL != nil && *ov.StrictSSL == c.opts.InsecureSkipVerify {
		transport = c.transport.Clone()
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: !*ov.StrictSSL} //nolint:gosec
		defer transport.CloseIdleConnections()
	}
	follow := c.opts.FollowRedirects
	if ov.FollowRedirects != nil {
		follow = *ov.FollowRedirects
	}
	maxRedirects := c.opts.MaxRedirects
	if ov.MaxRedirects != nil {
		maxRedirects = *ov.MaxRedirects
	}
	followOriginal := ov.FollowOriginalMethod != nil && *ov.FollowOriginalMethod
	removeReferer := ov.RemoveRefererOnRedirect != nil && *ov.RemoveRefererOnRedirect

	// Redirects are followed manually so that Postman's protocol profile
	// options (keep method, referer handling) can be honored.
	hc := &http.Client{
		Transport:     transport,
		Timeout:       c.opts.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	if !c.opts.DisableCookies && c.opts.Jar != nil {
		hc.Jar = c.opts.Jar
	}

	start := time.Now()
	send := func(method string, u *url.URL, h http.Header, body []byte) (*http.Response, *SentRequest, *Timings, error) {
		req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
		if err != nil {
			return nil, nil, nil, err
		}
		if len(body) == 0 {
			req.Body = http.NoBody
			req.ContentLength = 0
		}
		req.Header = h.Clone()
		if host != "" {
			req.Host = host
		}
		tm := &Timings{}
		var dnsStart, connStart, tlsStart time.Time
		reqStart := time.Now()
		trace := &httptrace.ClientTrace{
			DNSStart:          func(httptrace.DNSStartInfo) { dnsStart = time.Now() },
			DNSDone:           func(httptrace.DNSDoneInfo) { tm.DNS = ms(time.Since(dnsStart)) },
			ConnectStart:      func(string, string) { connStart = time.Now() },
			ConnectDone:       func(string, string, error) { tm.Connect = ms(time.Since(connStart)) },
			TLSHandshakeStart: func() { tlsStart = time.Now() },
			TLSHandshakeDone:  func(tls.ConnectionState, error) { tm.TLS = ms(time.Since(tlsStart)) },
			GotFirstResponseByte: func() {
				tm.FirstByte = ms(time.Since(reqStart))
			},
		}
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
		sent := &SentRequest{Method: method, URL: u.String(), Header: sortedHeaders(req.Header)}
		hostHeader := u.Host
		if host != "" {
			hostHeader = host
		}
		sent.Header = append(collection.Headers{{Key: "Host", Value: hostHeader}}, sent.Header...)
		if hc.Jar != nil {
			var cs []string
			for _, ck := range hc.Jar.Cookies(u) {
				cs = append(cs, ck.Name+"="+ck.Value)
			}
			if len(cs) > 0 {
				sent.Header = append(sent.Header, collection.Header{Key: "Cookie", Value: strings.Join(cs, "; ")})
			}
		}
		sent.Body = previewBody(body)
		resp, err := hc.Do(req)
		return resp, sent, tm, err
	}

	resp, sent, tm, err := send(method, u, header, body)
	if err == nil && authState.digest != nil && resp.StatusCode == http.StatusUnauthorized {
		if chal := resp.Header.Get("Www-Authenticate"); strings.HasPrefix(strings.ToLower(chal), "digest") {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if v, derr := authState.digest.authorize(chal, method, u, body); derr == nil {
				header = header.Clone()
				header.Set("Authorization", v)
			} else {
				warnings = append(warnings, "digest auth: "+derr.Error())
			}
			resp, sent, tm, err = send(method, u, header, body)
		}
	}

	var redirects []string
	curMethod, curURL, curHeader, curBody := method, u, header, body
	for err == nil && follow && isRedirect(resp.StatusCode) && resp.Header.Get("Location") != "" {
		if len(redirects) >= maxRedirects {
			resp.Body.Close()
			err = fmt.Errorf("exceeded maxRedirects (%d), probably stuck in a redirect loop", maxRedirects)
			break
		}
		next, perr := curURL.Parse(resp.Header.Get("Location"))
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if perr != nil {
			err = fmt.Errorf("invalid redirect location %q: %w", resp.Header.Get("Location"), perr)
			break
		}
		h := curHeader.Clone()
		switch code := resp.StatusCode; {
		case code == 307 || code == 308 || followOriginal:
		case curMethod != "HEAD":
			curMethod, curBody = "GET", nil
			h.Del("Content-Type")
			h.Del("Content-Length")
		}
		if next.Host != curURL.Host {
			h.Del("Authorization")
			host = ""
		}
		if !removeReferer {
			ref := *curURL
			ref.User = nil
			ref.Fragment = ""
			h.Set("Referer", ref.String())
		}
		redirects = append(redirects, next.String())
		curURL, curHeader = next, h
		resp, sent, tm, err = send(curMethod, curURL, curHeader, curBody)
	}
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("%s %s: %w", method, u.Redacted(), err)
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, c.opts.MaxResponseBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	tm.Total = ms(time.Since(start))
	if tm.FirstByte > 0 {
		tm.Download = tm.Total - tm.FirstByte
	}
	out := &Response{
		Code:      resp.StatusCode,
		Status:    statusText(resp),
		Proto:     resp.Proto,
		Header:    sortedHeaders(resp.Header),
		Time:      tm.Total,
		Timings:   *tm,
		Request:   *sent,
		Redirects: redirects,
		Warnings:  warnings,
	}
	if int64(len(raw)) > c.opts.MaxResponseBytes {
		raw = raw[:c.opts.MaxResponseBytes]
		out.Truncated = true
	}
	out.BodySize = int64(len(raw))
	if dec, derr := decode(resp.Header.Get("Content-Encoding"), raw); derr == nil {
		out.Body = dec
	} else {
		out.Body = raw
		out.Warnings = append(out.Warnings, "could not decode "+resp.Header.Get("Content-Encoding")+" body: "+derr.Error())
	}
	var hs int64
	for k, vs := range resp.Header {
		for _, v := range vs {
			hs += int64(len(k) + len(v) + 4)
		}
	}
	out.HeaderSize = hs
	return out, nil
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func statusText(resp *http.Response) string {
	s := resp.Status
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[i+1:]
	}
	return http.StatusText(resp.StatusCode)
}

func sortedHeaders(h http.Header) collection.Headers {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out collection.Headers
	for _, k := range keys {
		for _, v := range h[k] {
			out = append(out, collection.Header{Key: k, Value: v})
		}
	}
	return out
}

func previewBody(b []byte) string {
	const max = 64 << 10
	if len(b) > max {
		return string(b[:max]) + "\n…(truncated)"
	}
	return string(b)
}

func decode(enc string, b []byte) ([]byte, error) {
	var r io.Reader
	switch strings.ToLower(strings.TrimSpace(enc)) {
	case "", "identity":
		return b, nil
	case "gzip", "x-gzip":
		gr, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		r = gr
	case "deflate":
		r = flate.NewReader(bytes.NewReader(b))
	case "br":
		r = brotli.NewReader(bytes.NewReader(b))
	default:
		return b, nil
	}
	return io.ReadAll(r)
}

// ParseURL parses a Postman-style URL, defaulting the scheme to http.
func ParseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("URL is empty")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		// Retry with the query re-encoded: users type spaces etc.
		base, query, _ := strings.Cut(raw, "?")
		u2, err2 := url.Parse(base)
		if err2 != nil {
			return nil, fmt.Errorf("invalid URL %q: %w", raw, err)
		}
		u2.RawQuery = encodeQuery(query)
		return u2, nil
	}
	if u.Host == "" {
		return nil, fmt.Errorf("invalid URL %q: missing host", raw)
	}
	u.RawQuery = encodeQuery(u.RawQuery)
	return u, nil
}

// encodeQuery escapes characters that are invalid in a query while keeping
// the user's existing encoding (Postman sends the query mostly as typed).
func encodeQuery(q string) string {
	var sb strings.Builder
	for i := 0; i < len(q); i++ {
		ch := q[i]
		switch {
		case ch == '%' && i+2 < len(q) && isHex(q[i+1]) && isHex(q[i+2]):
			sb.WriteByte(ch)
		case ch <= ' ' || ch >= 0x7f || ch == '"' || ch == '<' || ch == '>' || ch == '`' || ch == '#' || ch == '%' || ch == '{' || ch == '}' || ch == '|' || ch == '\\' || ch == '^':
			fmt.Fprintf(&sb, "%%%02X", ch)
		default:
			sb.WriteByte(ch)
		}
	}
	return sb.String()
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func resolvePath(base, p string) string {
	if p == "" || filepath.IsAbs(p) || base == "" {
		return p
	}
	return filepath.Join(base, p)
}

// buildBody renders a body into bytes plus a default content type.
func buildBody(b *collection.Body, baseDir string) ([]byte, string, error) {
	if b == nil || b.Disabled {
		return nil, "", nil
	}
	switch b.Mode {
	case "raw":
		ct := map[string]string{
			"json":       "application/json",
			"xml":        "application/xml",
			"html":       "text/html",
			"javascript": "application/javascript",
			"text":       "text/plain",
		}[b.RawLanguage()]
		return []byte(b.Raw), ct, nil
	case "urlencoded":
		var parts []string
		for _, kv := range b.URLEncoded {
			if kv.Disabled || kv.Key == "" {
				continue
			}
			parts = append(parts, formEscape(kv.Key)+"="+formEscape(kv.Value))
		}
		return []byte(strings.Join(parts, "&")), "application/x-www-form-urlencoded", nil
	case "formdata":
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		for _, kv := range b.FormData {
			if kv.Disabled || kv.Key == "" {
				continue
			}
			if kv.Type == "file" {
				for _, f := range kv.Files() {
					path := resolvePath(baseDir, f)
					data, err := os.ReadFile(path)
					if err != nil {
						return nil, "", fmt.Errorf("form-data file %q: %w", f, err)
					}
					h := textproto.MIMEHeader{}
					h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, quoteEscape(kv.Key), quoteEscape(filepath.Base(path))))
					ct := kv.ContentType
					if ct == "" {
						ct = detectContentType(path, data)
					}
					h.Set("Content-Type", ct)
					pw, err := w.CreatePart(h)
					if err != nil {
						return nil, "", err
					}
					_, _ = pw.Write(data)
				}
				continue
			}
			h := textproto.MIMEHeader{}
			h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"`, quoteEscape(kv.Key)))
			if kv.ContentType != "" {
				h.Set("Content-Type", kv.ContentType)
			}
			pw, err := w.CreatePart(h)
			if err != nil {
				return nil, "", err
			}
			_, _ = pw.Write([]byte(kv.Value))
		}
		_ = w.Close()
		return buf.Bytes(), w.FormDataContentType(), nil
	case "file":
		if b.File == nil {
			return nil, "", nil
		}
		if b.File.Src == "" {
			return []byte(b.File.Content), "", nil
		}
		path := resolvePath(baseDir, b.File.Src)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, "", fmt.Errorf("body file %q: %w", b.File.Src, err)
		}
		return data, detectContentType(path, data), nil
	case "graphql":
		if b.GraphQL == nil {
			return nil, "", nil
		}
		payload := `{"query":` + jsonString(b.GraphQL.Query)
		if v := strings.TrimSpace(b.GraphQL.Variables); v != "" {
			payload += `,"variables":` + v
		}
		payload += "}"
		return []byte(payload), "application/json", nil
	}
	return nil, "", nil
}

func formEscape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "%7E", "~")
}

func quoteEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}

func detectContentType(path string, data []byte) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".csv":
		return "text/csv"
	case ".txt":
		return "text/plain"
	case ".svg":
		return "image/svg+xml"
	}
	return http.DetectContentType(data)
}

func jsonString(s string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&sb, `\u%04x`, r)
			} else {
				sb.WriteRune(r)
			}
		}
	}
	sb.WriteByte('"')
	return sb.String()
}
