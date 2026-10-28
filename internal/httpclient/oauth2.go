package httpclient

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bnuprst/httpman/internal/collection"
)

// PostmanBrowserCallback is the redirect URI Postman uses when "Authorize
// using browser" (useBrowser) is on; it is what providers have registered.
const PostmanBrowserCallback = "https://oauth.pstmn.io/v1/browser-callback"

// OAuth2Token is the result of a token request.
type OAuth2Token struct {
	AccessToken  string `json:"accessToken"`
	TokenType    string `json:"tokenType,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
	Scope        string `json:"scope,omitempty"`
	ExpiresIn    int64  `json:"expiresIn,omitempty"`
}

// OAuth2Authorizer sends the user to authURL (in a browser) and returns the
// URL the authorization server redirected back to.
type OAuth2Authorizer func(ctx context.Context, authURL, redirectURI string) (string, error)

// OAuth2RedirectURI returns the redirect URI a Postman OAuth 2.0 auth uses.
func OAuth2RedirectURI(a *collection.Auth) string {
	if a.Get("useBrowser") == "true" {
		return PostmanBrowserCallback
	}
	return a.Get("redirect_uri")
}

// FetchOAuth2Token obtains a token as configured by a (resolved) Postman
// OAuth 2.0 auth. With refreshToken set it refreshes instead. authorize is
// needed for the authorization code and implicit grants.
func (c *Client) FetchOAuth2Token(ctx context.Context, a *collection.Auth, refreshToken string, authorize OAuth2Authorizer) (*OAuth2Token, error) {
	if a == nil || a.Type != "oauth2" {
		return nil, errors.New("auth type is not OAuth 2.0")
	}
	form := url.Values{}
	if refreshToken != "" {
		form.Set("grant_type", "refresh_token")
		form.Set("refresh_token", refreshToken)
		return c.oauth2TokenRequest(ctx, a, defaultStr(a.Get("refreshTokenUrl"), a.Get("accessTokenUrl")), form, "refreshRequestParams")
	}
	grant := defaultStr(a.Get("grant_type"), "authorization_code")
	switch grant {
	case "client_credentials":
		form.Set("grant_type", "client_credentials")
	case "password_credentials":
		form.Set("grant_type", "password")
		form.Set("username", a.Get("username"))
		form.Set("password", a.Get("password"))
	case "authorization_code", "authorization_code_with_pkce", "implicit":
		return c.oauth2Interactive(ctx, a, grant, authorize)
	default:
		return nil, fmt.Errorf("unsupported grant type %q", grant)
	}
	if s := a.Get("scope"); s != "" {
		form.Set("scope", s)
	}
	return c.oauth2TokenRequest(ctx, a, a.Get("accessTokenUrl"), form, "tokenRequestParams")
}

func (c *Client) oauth2Interactive(ctx context.Context, a *collection.Auth, grant string, authorize OAuth2Authorizer) (*OAuth2Token, error) {
	if authorize == nil {
		return nil, errors.New("this grant type needs a browser")
	}
	authURL := a.Get("authUrl")
	if authURL == "" {
		return nil, errors.New("Auth URL is empty")
	}
	redirectURI := OAuth2RedirectURI(a)
	if redirectURI == "" {
		return nil, errors.New("Callback URL is empty")
	}
	u, err := url.Parse(authURL)
	if err != nil {
		return nil, fmt.Errorf("Auth URL: %w", err)
	}
	q := u.Query()
	if grant == "implicit" {
		q.Set("response_type", "token")
	} else {
		q.Set("response_type", "code")
	}
	q.Set("client_id", a.Get("clientId"))
	q.Set("redirect_uri", redirectURI)
	if s := a.Get("scope"); s != "" {
		q.Set("scope", s)
	}
	// A state is always sent and checked, so a stray request to the callback
	// cannot inject a code; the generated one is not stored in the auth.
	state := a.Get("state")
	if state == "" {
		state = randomHex(16)
	}
	q.Set("state", state)
	verifier := ""
	if grant == "authorization_code_with_pkce" {
		verifier = a.Get("code_verifier")
		if verifier == "" {
			verifier = pkceVerifier()
		}
		if strings.EqualFold(a.Get("challengeAlgorithm"), "plain") {
			q.Set("code_challenge", verifier)
			q.Set("code_challenge_method", "plain")
		} else {
			sum := sha256.Sum256([]byte(verifier))
			q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(sum[:]))
			q.Set("code_challenge_method", "S256")
		}
	}
	for _, p := range oauth2ExtraParams(a, "authRequestParams") {
		q.Set(p.key, p.value)
	}
	u.RawQuery = q.Encode()

	back, err := authorize(ctx, u.String(), redirectURI)
	if err != nil {
		return nil, err
	}
	params, err := redirectParams(back)
	if err != nil {
		return nil, err
	}
	if e := params.Get("error"); e != "" {
		return nil, oauth2Error(e, params.Get("error_description"))
	}
	if params.Get("state") != state {
		return nil, errors.New("state returned by the authorization server does not match")
	}
	if grant == "implicit" {
		tok := &OAuth2Token{AccessToken: params.Get("access_token"), TokenType: params.Get("token_type"), Scope: params.Get("scope")}
		tok.ExpiresIn, _ = strconv.ParseInt(params.Get("expires_in"), 10, 64)
		if tok.AccessToken == "" {
			return nil, errors.New("the redirect URL contains no access_token")
		}
		return tok, nil
	}
	code := params.Get("code")
	if code == "" {
		return nil, errors.New("the redirect URL contains no authorization code")
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}}
	if verifier != "" {
		form.Set("code_verifier", verifier)
	}
	return c.oauth2TokenRequest(ctx, a, a.Get("accessTokenUrl"), form, "tokenRequestParams")
}

func (c *Client) oauth2TokenRequest(ctx context.Context, a *collection.Auth, tokenURL string, form url.Values, extraKey string) (*OAuth2Token, error) {
	if tokenURL == "" {
		return nil, errors.New("Access Token URL is empty")
	}
	header := collection.Headers{{Key: "Accept", Value: "application/json"}}
	id, secret := a.Get("clientId"), a.Get("clientSecret")
	if a.Get("client_authentication") == "body" || secret == "" {
		if id != "" {
			form.Set("client_id", id)
		}
		if secret != "" {
			form.Set("client_secret", secret)
		}
	} else {
		cred := base64.StdEncoding.EncodeToString([]byte(id + ":" + secret))
		header = append(header, collection.Header{Key: "Authorization", Value: "Basic " + cred})
	}
	for _, p := range oauth2ExtraParams(a, extraKey) {
		switch p.sendAs {
		case "request_header":
			header = append(header, collection.Header{Key: p.key, Value: p.value})
		case "request_url":
			sep := "?"
			if strings.Contains(tokenURL, "?") {
				sep = "&"
			}
			tokenURL += sep + url.QueryEscape(p.key) + "=" + url.QueryEscape(p.value)
		default:
			form.Set(p.key, p.value)
		}
	}
	keys := make([]string, 0, len(form))
	for k := range form {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var kvs []collection.KV
	for _, k := range keys {
		for _, v := range form[k] {
			kvs = append(kvs, collection.KV{Key: k, Value: v})
		}
	}
	req := &Request{Method: "POST", URL: tokenURL, Header: header, Body: &collection.Body{Mode: "urlencoded", URLEncoded: kvs}}
	resp, err := c.Do(ctx, req, RequestOverrides{})
	if err != nil {
		return nil, err
	}
	return parseTokenResponse(resp)
}

func parseTokenResponse(resp *Response) (*OAuth2Token, error) {
	fields := map[string]string{}
	var obj map[string]any
	if json.Unmarshal(resp.Body, &obj) == nil {
		for k, v := range obj {
			fields[k] = collection.ValueString(v)
		}
	} else if q, err := url.ParseQuery(strings.TrimSpace(string(resp.Body))); err == nil {
		for k := range q {
			fields[k] = q.Get(k)
		}
	}
	if e := fields["error"]; e != "" {
		return nil, oauth2Error(e, fields["error_description"])
	}
	if resp.Code < 200 || resp.Code > 299 || fields["access_token"] == "" {
		body := strings.TrimSpace(string(resp.Body))
		if len(body) > 300 {
			body = body[:300] + "…"
		}
		if resp.Code < 200 || resp.Code > 299 {
			return nil, fmt.Errorf("token request failed: %s: %s", resp.Status, body)
		}
		return nil, fmt.Errorf("token response contains no access_token: %s", body)
	}
	tok := &OAuth2Token{AccessToken: fields["access_token"], TokenType: fields["token_type"], RefreshToken: fields["refresh_token"], Scope: fields["scope"]}
	tok.ExpiresIn, _ = strconv.ParseInt(fields["expires_in"], 10, 64)
	return tok, nil
}

func oauth2Error(code, desc string) error {
	if desc != "" {
		return fmt.Errorf("%s: %s", code, desc)
	}
	return errors.New(code)
}

// redirectParams reads the parameters of a redirect URL from both its query
// (authorization code) and its fragment (implicit).
func redirectParams(raw string) (url.Values, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid redirect URL: %w", err)
	}
	out := u.Query()
	if frag, err := url.ParseQuery(u.Fragment); err == nil {
		for k, vs := range frag {
			if out.Get(k) == "" {
				out[k] = vs
			}
		}
	}
	return out, nil
}

type oauth2Param struct{ key, value, sendAs string }

// oauth2ExtraParams reads Postman's advanced option lists (authRequestParams,
// tokenRequestParams, refreshRequestParams): [{key, value, enabled, send_as}].
func oauth2ExtraParams(a *collection.Auth, name string) []oauth2Param {
	var list []any
	for _, p := range a.Params[a.Type] {
		if p.Key == name {
			list, _ = p.Value.([]any)
		}
	}
	var out []oauth2Param
	for _, e := range list {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if en, ok := m["enabled"].(bool); ok && !en {
			continue
		}
		key := collection.ValueString(m["key"])
		if key == "" {
			continue
		}
		out = append(out, oauth2Param{key: key, value: collection.ValueString(m["value"]), sendAs: collection.ValueString(m["send_as"])})
	}
	return out
}

func pkceVerifier() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// RedirectListener captures an OAuth 2.0 redirect to a loopback address.
type RedirectListener struct {
	C   <-chan string
	srv *http.Server
}

// Close stops the listener, letting the page already being served finish.
func (l *RedirectListener) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if l.srv.Shutdown(ctx) != nil {
		_ = l.srv.Close()
	}
}

// The script moves an implicit grant's fragment into the query (keeping any
// query the callback URL already has) so that it reaches the listener.
const redirectPage = `<!doctype html><meta charset="utf-8"><title>httpman</title>
<body style="font-family:system-ui,sans-serif;padding:2em">
<p id="m">You can close this window and return to httpman.</p>
<script>if(location.hash.length>1){var h=location.hash.slice(1);location.replace(location.pathname+(location.search?location.search+"&":"?")+h)}</script>`

// ListenRedirect starts a listener for redirectURI when it points to this
// machine over plain http (http://localhost:port/path or 127.0.0.1). It
// returns nil, nil for any other redirect URI.
func ListenRedirect(redirectURI string) (*RedirectListener, error) {
	u, err := url.Parse(redirectURI)
	if err != nil || u.Scheme != "http" {
		return nil, nil
	}
	host := u.Hostname()
	var hosts []string
	switch host {
	case "localhost":
		// Browsers may connect to either address family.
		hosts = []string{"127.0.0.1", "::1"}
	case "127.0.0.1", "::1":
		hosts = []string{host}
	default:
		return nil, nil
	}
	port := u.Port()
	if port == "" {
		port = "80"
	}
	var lns []net.Listener
	var lerr error
	for _, h := range hosts {
		ln, err := net.Listen("tcp", net.JoinHostPort(h, port))
		if err != nil {
			lerr = err
			continue
		}
		lns = append(lns, ln)
	}
	if len(lns) == 0 {
		return nil, fmt.Errorf("cannot listen for the OAuth 2.0 callback on %s: %w", u.Host, lerr)
	}
	path := u.Path
	if path == "" {
		path = "/"
	}
	ch := make(chan string, 1)
	l := &RedirectListener{C: ch}
	l.srv = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(redirectPage))
		// Only an authorization response completes the flow. The first hit of
		// an implicit grant has its token in the fragment; the page script
		// sends it again as a query.
		q := r.URL.Query()
		if !q.Has("code") && !q.Has("access_token") && !q.Has("error") {
			return
		}
		got := *u
		got.RawQuery = r.URL.RawQuery
		select {
		case ch <- got.String():
		default:
		}
	})}
	for _, ln := range lns {
		go func(ln net.Listener) { _ = l.srv.Serve(ln) }(ln)
	}
	return l, nil
}
