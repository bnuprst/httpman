package httpclient

import (
	"compress/gzip"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/bnuprst/httpman/internal/collection"
)

func md5hex(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func TestDigestAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="test", qop="auth", nonce="abc", opaque="xyz"`)
			w.WriteHeader(401)
			return
		}
		c := parseChallenge(auth)
		ha1 := md5hex("u:test:p")
		ha2 := md5hex(r.Method + ":" + c["uri"])
		want := md5hex(ha1 + ":abc:" + c["nc"] + ":" + c["cnonce"] + ":auth:" + ha2)
		if c["response"] != want {
			w.WriteHeader(403)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()
	auth := &collection.Auth{Type: "digest", Params: map[string][]collection.AuthParam{"digest": {{Key: "username", Value: "u"}, {Key: "password", Value: "p"}}}}
	resp, err := New(DefaultOptions()).Do(context.Background(), &Request{Method: "GET", URL: srv.URL + "/x?y=1", Auth: auth}, RequestOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Code != 200 || string(resp.Body) != "ok" {
		t.Fatalf("got %d %s", resp.Code, resp.Body)
	}
}

func TestFormDataAndGzip(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("file-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			w.WriteHeader(400)
			return
		}
		f, fh, err := r.FormFile("upload")
		if err != nil {
			w.WriteHeader(400)
			return
		}
		b := make([]byte, 100)
		n, _ := f.Read(b)
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		fmt.Fprintf(gz, "%s|%s|%s", r.FormValue("name"), fh.Filename, b[:n])
		gz.Close()
	}))
	defer srv.Close()
	opts := DefaultOptions()
	opts.BaseDir = dir
	body := &collection.Body{Mode: "formdata", FormData: []collection.KV{
		{Key: "name", Value: "ada", Type: "text"},
		{Key: "upload", Type: "file", Src: "a.txt"},
		{Key: "off", Value: "x", Disabled: true},
	}}
	resp, err := New(opts).Do(context.Background(), &Request{Method: "POST", URL: srv.URL, Body: body}, RequestOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "ada|a.txt|file-content" {
		t.Fatalf("got %d %q", resp.Code, resp.Body)
	}
}

func TestParseURL(t *testing.T) {
	for in, want := range map[string]string{
		"example.com/a?b=c d":        "http://example.com/a?b=c%20d",
		"https://x.io/p?q=%20&r={a}": "https://x.io/p?q=%20&r=%7Ba%7D",
	} {
		u, err := ParseURL(in)
		if err != nil {
			t.Fatal(err)
		}
		if u.String() != want {
			t.Errorf("%s → %s, want %s", in, u, want)
		}
	}
}
