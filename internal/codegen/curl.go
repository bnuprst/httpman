// Package codegen converts between requests and code: cURL import, and
// snippet generation for several languages.
package codegen

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/bnuprst/httpman/internal/collection"
)

// splitShell splits a command line honoring single/double quotes,
// backslash escapes, $'..' strings and line continuations (\, ^ and `).
func splitShell(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inArg := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case (c == '\\' || c == '^' || c == '`') && i+1 < len(s) && (s[i+1] == '\n' || s[i+1] == '\r'):
			i++
			if i+1 < len(s) && s[i] == '\r' && s[i+1] == '\n' {
				i++
			}
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		case c == '\'':
			inArg = true
			end := strings.IndexByte(s[i+1:], '\'')
			if end < 0 {
				return nil, errors.New("unterminated single quote")
			}
			cur.WriteString(s[i+1 : i+1+end])
			i += end + 1
		case c == '$' && i+1 < len(s) && s[i+1] == '\'':
			inArg = true
			i += 2
			for ; i < len(s) && s[i] != '\''; i++ {
				if s[i] == '\\' && i+1 < len(s) {
					i++
					switch s[i] {
					case 'n':
						cur.WriteByte('\n')
					case 't':
						cur.WriteByte('\t')
					case 'r':
						cur.WriteByte('\r')
					default:
						cur.WriteByte(s[i])
					}
					continue
				}
				cur.WriteByte(s[i])
			}
		case c == '"':
			inArg = true
			i++
			for ; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) && strings.IndexByte("\"\\$`\n", s[i+1]) >= 0 {
					i++
				}
				cur.WriteByte(s[i])
			}
			if i >= len(s) {
				return nil, errors.New("unterminated double quote")
			}
		case c == '\\' && i+1 < len(s):
			inArg = true
			i++
			cur.WriteByte(s[i])
		default:
			inArg = true
			cur.WriteByte(c)
		}
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args, nil
}

// ParseCurl converts a curl command into a Postman request.
func ParseCurl(cmd string) (*collection.Request, error) {
	args, err := splitShell(strings.TrimSpace(cmd))
	if err != nil {
		return nil, err
	}
	if len(args) == 0 || !strings.HasSuffix(strings.ToLower(args[0]), "curl") && !strings.HasSuffix(strings.ToLower(args[0]), "curl.exe") {
		return nil, errors.New("not a curl command")
	}
	req := &collection.Request{Header: collection.Headers{}}
	var data []string
	var form []collection.KV
	var urlencodedGet bool
	rawURL := ""
	method := ""
	user := ""
	isBinary := false
	next := func(i *int) string {
		if *i+1 < len(args) {
			*i++
			return args[*i]
		}
		return ""
	}
	for i := 1; i < len(args); i++ {
		a := args[i]
		// --flag=value form
		if strings.HasPrefix(a, "--") && strings.Contains(a, "=") {
			k, v, _ := strings.Cut(a, "=")
			args = append(args[:i], append([]string{k, v}, args[i+1:]...)...)
			a = k
		}
		switch a {
		case "-X", "--request":
			method = strings.ToUpper(next(&i))
		case "-H", "--header":
			h := next(&i)
			k, v, _ := strings.Cut(h, ":")
			req.Header = append(req.Header, collection.Header{Key: strings.TrimSpace(k), Value: strings.TrimSpace(v)})
		case "-d", "--data", "--data-ascii", "--data-raw":
			data = append(data, next(&i))
		case "--data-binary":
			data = append(data, next(&i))
			isBinary = true
		case "--data-urlencode":
			v := next(&i)
			if k, val, ok := strings.Cut(v, "="); ok {
				data = append(data, k+"="+url.QueryEscape(val))
			} else {
				data = append(data, url.QueryEscape(v))
			}
		case "--json":
			data = append(data, next(&i))
			req.Header = append(req.Header, collection.Header{Key: "Content-Type", Value: "application/json"}, collection.Header{Key: "Accept", Value: "application/json"})
		case "-F", "--form", "--form-string":
			v := next(&i)
			k, val, _ := strings.Cut(v, "=")
			kv := collection.KV{Key: k, Type: "text", Value: val}
			if strings.HasPrefix(val, "@") && a != "--form-string" {
				path, _, _ := strings.Cut(val[1:], ";")
				kv = collection.KV{Key: k, Type: "file", Src: path}
			}
			form = append(form, kv)
		case "-u", "--user":
			user = next(&i)
		case "-A", "--user-agent":
			req.Header = append(req.Header, collection.Header{Key: "User-Agent", Value: next(&i)})
		case "-e", "--referer":
			req.Header = append(req.Header, collection.Header{Key: "Referer", Value: next(&i)})
		case "-b", "--cookie":
			req.Header = append(req.Header, collection.Header{Key: "Cookie", Value: next(&i)})
		case "-I", "--head":
			method = "HEAD"
		case "-G", "--get":
			urlencodedGet = true
		case "--url":
			rawURL = next(&i)
		case "-o", "--output", "-w", "--write-out", "--connect-timeout", "-m", "--max-time", "--retry", "-x", "--proxy", "--cacert", "--cert", "--key", "-T", "--upload-file", "-c", "--cookie-jar", "--max-redirs":
			next(&i)
		default:
			if !strings.HasPrefix(a, "-") && rawURL == "" {
				rawURL = a
			}
		}
	}
	if rawURL == "" {
		return nil, errors.New("curl command has no URL")
	}
	if urlencodedGet && len(data) > 0 {
		sep := "?"
		if strings.Contains(rawURL, "?") {
			sep = "&"
		}
		rawURL += sep + strings.Join(data, "&")
		data = nil
	}
	req.URL = collection.URL{Raw: rawURL}
	if u, err := url.Parse(rawURL); err == nil {
		for _, part := range strings.Split(u.RawQuery, "&") {
			if part == "" {
				continue
			}
			k, v, _ := strings.Cut(part, "=")
			kk, vv := k, v
			req.URL.Query = append(req.URL.Query, collection.QueryParam{Key: &kk, Value: &vv})
		}
	}
	if user != "" {
		u, p, _ := strings.Cut(user, ":")
		req.Auth = &collection.Auth{Type: "basic", Params: map[string][]collection.AuthParam{"basic": {{Key: "username", Value: u, Type: "string"}, {Key: "password", Value: p, Type: "string"}}}}
	}
	switch {
	case len(form) > 0:
		req.Body = &collection.Body{Mode: "formdata", FormData: form}
	case len(data) > 0:
		body := strings.Join(data, "&")
		ct := ""
		for _, h := range req.Header {
			if strings.EqualFold(h.Key, "Content-Type") {
				ct = strings.ToLower(h.Value)
			}
		}
		if (ct == "" || strings.Contains(ct, "x-www-form-urlencoded")) && !isBinary && looksURLEncoded(body) {
			b := &collection.Body{Mode: "urlencoded"}
			for _, part := range strings.Split(body, "&") {
				k, v, _ := strings.Cut(part, "=")
				uk, err1 := url.QueryUnescape(k)
				uv, err2 := url.QueryUnescape(v)
				if err1 != nil || err2 != nil {
					uk, uv = k, v
				}
				b.URLEncoded = append(b.URLEncoded, collection.KV{Key: uk, Value: uv})
			}
			req.Body = b
		} else {
			lang := "text"
			if strings.Contains(ct, "json") || (ct == "" && (strings.HasPrefix(strings.TrimSpace(body), "{") || strings.HasPrefix(strings.TrimSpace(body), "["))) {
				lang = "json"
			} else if strings.Contains(ct, "xml") {
				lang = "xml"
			}
			req.Body = &collection.Body{Mode: "raw", Raw: body, Options: []byte(fmt.Sprintf(`{"raw":{"language":%q}}`, lang))}
		}
	}
	if method == "" {
		method = "GET"
		if req.Body != nil {
			method = "POST"
		}
	}
	req.Method = method
	return req, nil
}

func looksURLEncoded(s string) bool {
	if s == "" || strings.ContainsAny(s, "\n{}[]\"<>") {
		return false
	}
	for _, part := range strings.Split(s, "&") {
		if !strings.Contains(part, "=") {
			return false
		}
	}
	return true
}

// ---- generation ----

// Snippet input: a resolved request.
type Snippet struct {
	Method string             `json:"method"`
	URL    string             `json:"url"`
	Header collection.Headers `json:"header"`
	Body   *collection.Body   `json:"body,omitempty"`
	Auth   *collection.Auth   `json:"auth,omitempty"`
}

func (s *Snippet) headers() collection.Headers {
	h := append(collection.Headers{}, s.Header...)
	if s.Auth != nil {
		switch s.Auth.Type {
		case "basic":
			h = append(h, collection.Header{Key: "Authorization", Value: "Basic " + base64.StdEncoding.EncodeToString([]byte(s.Auth.Get("username")+":"+s.Auth.Get("password")))})
		case "bearer":
			h = append(h, collection.Header{Key: "Authorization", Value: "Bearer " + s.Auth.Get("token")})
		case "apikey":
			if s.Auth.Get("in") != "query" {
				h = append(h, collection.Header{Key: s.Auth.Get("key"), Value: s.Auth.Get("value")})
			}
		case "oauth2":
			h = append(h, collection.Header{Key: "Authorization", Value: strings.TrimSpace(defaultStr(s.Auth.Get("headerPrefix"), "Bearer") + " " + s.Auth.Get("accessToken"))})
		}
	}
	if s.Body != nil {
		ct := ""
		switch s.Body.Mode {
		case "raw":
			ct = map[string]string{"json": "application/json", "xml": "application/xml", "html": "text/html", "javascript": "application/javascript"}[s.Body.RawLanguage()]
		case "urlencoded":
			ct = "application/x-www-form-urlencoded"
		case "graphql":
			ct = "application/json"
		}
		if ct != "" && !hasHeader(h, "Content-Type") {
			h = append(h, collection.Header{Key: "Content-Type", Value: ct})
		}
	}
	return h
}

func defaultStr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func hasHeader(h collection.Headers, k string) bool {
	for _, x := range h {
		if strings.EqualFold(x.Key, k) {
			return true
		}
	}
	return false
}

func (s *Snippet) url() string {
	if s.Auth != nil && s.Auth.Type == "apikey" && s.Auth.Get("in") == "query" {
		sep := "?"
		if strings.Contains(s.URL, "?") {
			sep = "&"
		}
		return s.URL + sep + url.QueryEscape(s.Auth.Get("key")) + "=" + url.QueryEscape(s.Auth.Get("value"))
	}
	return s.URL
}

func (s *Snippet) rawBody() string {
	if s.Body == nil {
		return ""
	}
	switch s.Body.Mode {
	case "raw":
		return s.Body.Raw
	case "urlencoded":
		var parts []string
		for _, kv := range s.Body.URLEncoded {
			if !kv.Disabled {
				parts = append(parts, url.QueryEscape(kv.Key)+"="+url.QueryEscape(kv.Value))
			}
		}
		return strings.Join(parts, "&")
	case "graphql":
		if s.Body.GraphQL == nil {
			return ""
		}
		v := strings.TrimSpace(s.Body.GraphQL.Variables)
		if v == "" {
			v = "{}"
		}
		return fmt.Sprintf(`{"query":%q,"variables":%s}`, s.Body.GraphQL.Query, v)
	}
	return ""
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// Languages lists supported snippet targets.
var Languages = []struct{ ID, Name string }{
	{"curl", "cURL (bash)"},
	{"curl-cmd", "cURL (Windows cmd)"},
	{"powershell", "PowerShell"},
	{"fetch", "JavaScript fetch"},
	{"python", "Python requests"},
	{"httpie", "HTTPie"},
	{"http", "Raw HTTP"},
}

// Generate renders a snippet in the given language.
func Generate(s *Snippet, lang string) (string, error) {
	h := s.headers()
	body := s.rawBody()
	switch lang {
	case "curl", "curl-cmd":
		q := shQuote
		nl := " \\\n  "
		if lang == "curl-cmd" {
			q = func(v string) string {
				return `"` + strings.ReplaceAll(strings.ReplaceAll(v, `"`, `\"`), "%", "%%") + `"`
			}
			nl = " ^\n  "
		}
		var b strings.Builder
		b.WriteString("curl --location --request " + s.Method + " " + q(s.url()))
		for _, x := range h {
			b.WriteString(nl + "--header " + q(x.Key+": "+x.Value))
		}
		if s.Body != nil && s.Body.Mode == "formdata" {
			for _, kv := range s.Body.FormData {
				if kv.Disabled {
					continue
				}
				if kv.Type == "file" {
					for _, f := range kv.Files() {
						b.WriteString(nl + "--form " + q(kv.Key+"=@"+f))
					}
				} else {
					b.WriteString(nl + "--form " + q(kv.Key+"="+kv.Value))
				}
			}
		} else if s.Body != nil && s.Body.Mode == "file" && s.Body.File != nil {
			b.WriteString(nl + "--data-binary " + q("@"+s.Body.File.Src))
		} else if body != "" {
			b.WriteString(nl + "--data-raw " + q(body))
		}
		return b.String(), nil
	case "powershell":
		var b strings.Builder
		b.WriteString("$headers = New-Object \"System.Collections.Generic.Dictionary[[String],[String]]\"\n")
		for _, x := range h {
			b.WriteString(fmt.Sprintf("$headers.Add(%s, %s)\n", psQuote(x.Key), psQuote(x.Value)))
		}
		b.WriteString("\n")
		if body != "" {
			b.WriteString("$body = " + psQuote(body) + "\n\n")
			b.WriteString(fmt.Sprintf("$response = Invoke-RestMethod %s -Method '%s' -Headers $headers -Body $body\n", psQuote(s.url()), s.Method))
		} else {
			b.WriteString(fmt.Sprintf("$response = Invoke-RestMethod %s -Method '%s' -Headers $headers\n", psQuote(s.url()), s.Method))
		}
		b.WriteString("$response | ConvertTo-Json")
		return b.String(), nil
	case "fetch":
		var b strings.Builder
		b.WriteString("const headers = new Headers();\n")
		for _, x := range h {
			b.WriteString(fmt.Sprintf("headers.append(%q, %q);\n", x.Key, x.Value))
		}
		opts := fmt.Sprintf("  method: %q,\n  headers,\n", s.Method)
		if s.Body != nil && s.Body.Mode == "formdata" {
			b.WriteString("\nconst body = new FormData();\n")
			for _, kv := range s.Body.FormData {
				if !kv.Disabled {
					if kv.Type == "file" {
						b.WriteString(fmt.Sprintf("body.append(%q, fileInput.files[0], %q);\n", kv.Key, strings.Join(kv.Files(), ",")))
					} else {
						b.WriteString(fmt.Sprintf("body.append(%q, %q);\n", kv.Key, kv.Value))
					}
				}
			}
			opts += "  body,\n"
		} else if body != "" {
			b.WriteString(fmt.Sprintf("\nconst body = %s;\n", jsString(body)))
			opts += "  body,\n"
		}
		b.WriteString(fmt.Sprintf("\nfetch(%q, {\n%s  redirect: \"follow\",\n})\n  .then((response) => response.text())\n  .then((result) => console.log(result))\n  .catch((error) => console.error(error));", s.url(), opts))
		return b.String(), nil
	case "python":
		var b strings.Builder
		b.WriteString("import requests\n\n")
		b.WriteString(fmt.Sprintf("url = %s\n\n", pyString(s.url())))
		b.WriteString("headers = {\n")
		for _, x := range h {
			b.WriteString(fmt.Sprintf("    %s: %s,\n", pyString(x.Key), pyString(x.Value)))
		}
		b.WriteString("}\n")
		args := "headers=headers"
		if s.Body != nil && s.Body.Mode == "formdata" {
			b.WriteString("data = {\n")
			var files []string
			for _, kv := range s.Body.FormData {
				if kv.Disabled {
					continue
				}
				if kv.Type == "file" {
					for _, f := range kv.Files() {
						files = append(files, fmt.Sprintf("    (%s, open(%s, \"rb\")),", pyString(kv.Key), pyString(f)))
					}
				} else {
					b.WriteString(fmt.Sprintf("    %s: %s,\n", pyString(kv.Key), pyString(kv.Value)))
				}
			}
			b.WriteString("}\n")
			args += ", data=data"
			if len(files) > 0 {
				b.WriteString("files = [\n" + strings.Join(files, "\n") + "\n]\n")
				args += ", files=files"
			}
		} else if body != "" {
			b.WriteString(fmt.Sprintf("payload = %s\n", pyString(body)))
			args += ", data=payload"
		}
		b.WriteString(fmt.Sprintf("\nresponse = requests.request(%s, url, %s)\n\nprint(response.text)", pyString(s.Method), args))
		return b.String(), nil
	case "httpie":
		var b strings.Builder
		if body != "" {
			b.WriteString("printf %s " + shQuote(body) + " | ")
		}
		b.WriteString("http --follow " + s.Method + " " + shQuote(s.url()))
		for _, x := range h {
			b.WriteString(" \\\n  " + shQuote(x.Key+":"+x.Value))
		}
		return b.String(), nil
	case "http":
		u, err := url.Parse(s.url())
		if err != nil {
			return "", err
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("%s %s HTTP/1.1\nHost: %s\n", s.Method, u.RequestURI(), u.Host))
		for _, x := range h {
			b.WriteString(x.Key + ": " + x.Value + "\n")
		}
		if body != "" {
			b.WriteString(fmt.Sprintf("Content-Length: %d\n\n%s", len(body), body))
		}
		return b.String(), nil
	}
	return "", fmt.Errorf("unknown language %q", lang)
}

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func jsString(s string) string {
	if !strings.Contains(s, "`") && !strings.Contains(s, "${") && strings.Contains(s, "\n") {
		return "`" + strings.ReplaceAll(s, `\`, `\\`) + "`"
	}
	return fmt.Sprintf("%q", s)
}

func pyString(s string) string {
	if strings.Contains(s, "\n") && !strings.Contains(s, `"""`) {
		return `"""` + strings.ReplaceAll(s, `\`, `\\`) + `"""`
	}
	return fmt.Sprintf("%q", s)
}
