package httpclient

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/bnuprst/httpman/internal/collection"
)

func oauth2Auth(t *testing.T, params string) *collection.Auth {
	t.Helper()
	var a collection.Auth
	if err := json.Unmarshal([]byte(`{"type":"oauth2","oauth2":`+params+`}`), &a); err != nil {
		t.Fatal(err)
	}
	return &a
}

// tokenServer records the last token request and answers with a token.
type tokenServer struct {
	*httptest.Server
	form   url.Values
	query  url.Values
	header http.Header
}

func newTokenServer(t *testing.T, respond func(w http.ResponseWriter, form url.Values)) *tokenServer {
	ts := &tokenServer{}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		ts.form, ts.query, ts.header = r.PostForm, r.URL.Query(), r.Header
		if respond != nil {
			respond(w, r.PostForm)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"tok","token_type":"Bearer","expires_in":3600,"refresh_token":"ref"}`)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestOAuth2ClientCredentials(t *testing.T) {
	ts := newTokenServer(t, nil)
	c := New(DefaultOptions())
	a := oauth2Auth(t, `[{"key":"grant_type","value":"client_credentials"},{"key":"accessTokenUrl","value":"`+ts.URL+`/token"},
		{"key":"clientId","value":"id"},{"key":"clientSecret","value":"s3cret"},{"key":"scope","value":"read write"},
		{"key":"tokenRequestParams","value":[
			{"key":"audience","value":"api://x","enabled":true,"send_as":"request_body"},
			{"key":"X-Origin","value":"o","enabled":true,"send_as":"request_header"},
			{"key":"resource","value":"r","enabled":true,"send_as":"request_url"},
			{"key":"skipped","value":"no","enabled":false,"send_as":"request_body"}]}]`)
	tok, err := c.FetchOAuth2Token(context.Background(), a, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "tok" || tok.TokenType != "Bearer" || tok.ExpiresIn != 3600 || tok.RefreshToken != "ref" {
		t.Errorf("token: %+v", tok)
	}
	if ts.header.Get("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("id:s3cret")) {
		t.Errorf("client auth header: %q", ts.header.Get("Authorization"))
	}
	if ts.form.Get("grant_type") != "client_credentials" || ts.form.Get("scope") != "read write" || ts.form.Get("client_secret") != "" {
		t.Errorf("form: %v", ts.form)
	}
	if ts.form.Get("audience") != "api://x" || ts.header.Get("X-Origin") != "o" || ts.query.Get("resource") != "r" || ts.form.Has("skipped") {
		t.Errorf("extra params: form=%v header=%v query=%v", ts.form, ts.header, ts.query)
	}

	// Credentials in the body instead of a Basic header.
	a = oauth2Auth(t, `[{"key":"grant_type","value":"client_credentials"},{"key":"accessTokenUrl","value":"`+ts.URL+`"},
		{"key":"clientId","value":"id"},{"key":"clientSecret","value":"s3cret"},{"key":"client_authentication","value":"body"}]`)
	if _, err := c.FetchOAuth2Token(context.Background(), a, "", nil); err != nil {
		t.Fatal(err)
	}
	if ts.header.Get("Authorization") != "" || ts.form.Get("client_id") != "id" || ts.form.Get("client_secret") != "s3cret" {
		t.Errorf("body auth: header=%q form=%v", ts.header.Get("Authorization"), ts.form)
	}
}

func TestOAuth2PasswordAndRefresh(t *testing.T) {
	ts := newTokenServer(t, nil)
	c := New(DefaultOptions())
	a := oauth2Auth(t, `[{"key":"grant_type","value":"password_credentials"},{"key":"accessTokenUrl","value":"`+ts.URL+`/token"},
		{"key":"refreshTokenUrl","value":"`+ts.URL+`/refresh"},{"key":"clientId","value":"id"},
		{"key":"username","value":"u"},{"key":"password","value":"p"}]`)
	if _, err := c.FetchOAuth2Token(context.Background(), a, "", nil); err != nil {
		t.Fatal(err)
	}
	// No secret: the client id goes in the body (public client).
	if ts.form.Get("grant_type") != "password" || ts.form.Get("username") != "u" || ts.form.Get("password") != "p" || ts.form.Get("client_id") != "id" {
		t.Errorf("password form: %v", ts.form)
	}
	if _, err := c.FetchOAuth2Token(context.Background(), a, "ref", nil); err != nil {
		t.Fatal(err)
	}
	if ts.form.Get("grant_type") != "refresh_token" || ts.form.Get("refresh_token") != "ref" {
		t.Errorf("refresh form: %v", ts.form)
	}
}

func TestOAuth2Errors(t *testing.T) {
	c := New(DefaultOptions())
	ts := newTokenServer(t, func(w http.ResponseWriter, _ url.Values) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":"invalid_client","error_description":"bad secret"}`)
	})
	a := oauth2Auth(t, `[{"key":"grant_type","value":"client_credentials"},{"key":"accessTokenUrl","value":"`+ts.URL+`"}]`)
	if _, err := c.FetchOAuth2Token(context.Background(), a, "", nil); err == nil || err.Error() != "invalid_client: bad secret" {
		t.Errorf("error: %v", err)
	}

	// GitHub answers form-encoded unless asked for JSON.
	form := newTokenServer(t, func(w http.ResponseWriter, _ url.Values) {
		fmt.Fprint(w, "access_token=gh&token_type=bearer&scope=repo")
	})
	a = oauth2Auth(t, `[{"key":"grant_type","value":"client_credentials"},{"key":"accessTokenUrl","value":"`+form.URL+`"}]`)
	if tok, err := c.FetchOAuth2Token(context.Background(), a, "", nil); err != nil || tok.AccessToken != "gh" || tok.Scope != "repo" {
		t.Errorf("form response: %+v %v", tok, err)
	}
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// browser follows redirects like a user agent would, including running the
// loopback page's fragment-to-query script by hand.
func browser(t *testing.T) OAuth2Authorizer {
	return func(ctx context.Context, authURL, redirectURI string) (string, error) {
		l, err := ListenRedirect(redirectURI)
		if err != nil || l == nil {
			t.Fatalf("listen %s: %v", redirectURI, err)
		}
		defer l.Close()
		resp, err := http.Get(authURL)
		if err != nil {
			return "", err
		}
		resp.Body.Close()
		if frag := resp.Request.URL.Fragment; frag != "" {
			u := *resp.Request.URL
			u.Fragment, u.RawQuery = "", frag
			r2, err := http.Get(u.String())
			if err != nil {
				return "", err
			}
			r2.Body.Close()
		}
		return <-l.C, nil
	}
}

func TestOAuth2AuthorizationCodePKCE(t *testing.T) {
	redirect := fmt.Sprintf("http://127.0.0.1:%d/callback", freePort(t))
	var challenge string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
			q := r.URL.Query()
			if q.Get("response_type") != "code" || q.Get("client_id") != "id" || q.Get("redirect_uri") != redirect || q.Get("code_challenge_method") != "S256" || q.Get("prompt") != "login" {
				t.Errorf("authorize query: %v", q)
			}
			challenge = q.Get("code_challenge")
			http.Redirect(w, r, redirect+"?code=abc&state="+q.Get("state"), http.StatusFound)
		case "/token":
			_ = r.ParseForm()
			sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
			if r.PostForm.Get("code") != "abc" || r.PostForm.Get("redirect_uri") != redirect || base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
				t.Errorf("token form: %v (challenge %s)", r.PostForm, challenge)
			}
			fmt.Fprint(w, `{"access_token":"code-tok","token_type":"Bearer"}`)
		}
	}))
	defer srv.Close()
	a := oauth2Auth(t, `[{"key":"grant_type","value":"authorization_code_with_pkce"},{"key":"authUrl","value":"`+srv.URL+`/authorize"},
		{"key":"accessTokenUrl","value":"`+srv.URL+`/token"},{"key":"redirect_uri","value":"`+redirect+`"},
		{"key":"clientId","value":"id"},{"key":"clientSecret","value":""},{"key":"state","value":"xyz"},
		{"key":"authRequestParams","value":[{"key":"prompt","value":"login","enabled":true,"send_as":"request_url"}]}]`)
	tok, err := New(DefaultOptions()).FetchOAuth2Token(context.Background(), a, "", browser(t))
	if err != nil || tok.AccessToken != "code-tok" {
		t.Fatalf("token: %+v %v", tok, err)
	}
}

func TestOAuth2Implicit(t *testing.T) {
	redirect := fmt.Sprintf("http://localhost:%d/cb", freePort(t))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirect+"#access_token=imp&token_type=Bearer&expires_in=60&state="+r.URL.Query().Get("state"), http.StatusFound)
	}))
	defer srv.Close()
	a := oauth2Auth(t, `[{"key":"grant_type","value":"implicit"},{"key":"authUrl","value":"`+srv.URL+`"},
		{"key":"redirect_uri","value":"`+redirect+`"},{"key":"clientId","value":"id"},{"key":"state","value":"s"}]`)
	tok, err := New(DefaultOptions()).FetchOAuth2Token(context.Background(), a, "", browser(t))
	if err != nil || tok.AccessToken != "imp" || tok.ExpiresIn != 60 {
		t.Fatalf("token: %+v %v", tok, err)
	}

	// A pasted redirect URL is parsed the same way; a wrong state is rejected.
	paste := func(u string) OAuth2Authorizer {
		return func(context.Context, string, string) (string, error) { return u, nil }
	}
	a = oauth2Auth(t, `[{"key":"grant_type","value":"implicit"},{"key":"authUrl","value":"https://idp/auth"},{"key":"useBrowser","value":true},{"key":"state","value":"s"}]`)
	if OAuth2RedirectURI(a) != PostmanBrowserCallback {
		t.Errorf("useBrowser redirect: %s", OAuth2RedirectURI(a))
	}
	if tok, err := New(DefaultOptions()).FetchOAuth2Token(context.Background(), a, "", paste(PostmanBrowserCallback+"#access_token=p&state=s")); err != nil || tok.AccessToken != "p" {
		t.Errorf("pasted: %+v %v", tok, err)
	}
	if _, err := New(DefaultOptions()).FetchOAuth2Token(context.Background(), a, "", paste(PostmanBrowserCallback+"#access_token=p&state=evil")); err == nil || !strings.Contains(err.Error(), "state") {
		t.Errorf("state mismatch accepted: %v", err)
	}
}

func TestOAuth2ApplyMatchesPostman(t *testing.T) {
	apply := func(params string) (http.Header, *url.URL) {
		u, _ := url.Parse("https://x/y")
		h := http.Header{}
		var w []string
		if _, err := applyAuth(oauth2Auth(t, params), "GET", u, h, nil, &w); err != nil {
			t.Fatal(err)
		}
		return h, u
	}
	if h, _ := apply(`[{"key":"accessToken","value":"t"}]`); h.Get("Authorization") != "Bearer t" {
		t.Errorf("default: %q", h.Get("Authorization"))
	}
	if h, _ := apply(`[{"key":"accessToken","value":"t"},{"key":"headerPrefix","value":""}]`); h.Get("Authorization") != "t" {
		t.Errorf("empty prefix: %q", h.Get("Authorization"))
	}
	if h, _ := apply(`[{"key":"accessToken","value":"t"},{"key":"tokenType","value":"mac"}]`); h.Get("Authorization") != "" {
		t.Errorf("mac token applied: %q", h.Get("Authorization"))
	}
	if _, u := apply(`[{"key":"accessToken","value":"t"},{"key":"addTokenTo","value":"queryParams"}]`); u.Query().Get("access_token") != "t" {
		t.Errorf("query: %s", u)
	}
}
