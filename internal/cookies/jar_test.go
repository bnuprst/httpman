package cookies

import (
	"net/http"
	"net/url"
	"path/filepath"
	"testing"
)

func TestJar(t *testing.T) {
	j := New()
	u, _ := url.Parse("https://api.example.com/v1/users")
	j.SetCookies(u, []*http.Cookie{
		{Name: "host", Value: "1"},
		{Name: "dom", Value: "2", Domain: ".example.com", Path: "/"},
		{Name: "sec", Value: "3", Secure: true, Path: "/"},
		{Name: "bad", Value: "4", Domain: "other.com"},
		{Name: "psl", Value: "5", Domain: "com"},
	})
	names := func(raw string) map[string]bool {
		u, _ := url.Parse(raw)
		out := map[string]bool{}
		for _, c := range j.Cookies(u) {
			out[c.Name] = true
		}
		return out
	}
	if got := names("https://api.example.com/v1/x"); !got["host"] || !got["dom"] || !got["sec"] || got["bad"] || got["psl"] {
		t.Errorf("same host: %v", got)
	}
	if got := names("http://www.example.com/"); got["host"] || !got["dom"] || got["sec"] {
		t.Errorf("sibling host over http: %v", got)
	}
	if got := names("https://api.example.com/other"); got["host"] {
		t.Errorf("path scoping: %v", got)
	}
	p := filepath.Join(t.TempDir(), "c.json")
	if err := j.Save(p); err != nil {
		t.Fatal(err)
	}
	j2 := New()
	if err := j2.Load(p); err != nil || len(j2.All()) != 3 {
		t.Fatalf("reload: %v %+v", err, j2.All())
	}
	j2.SetCookies(u, []*http.Cookie{{Name: "dom", Domain: ".example.com", Path: "/", MaxAge: -1}})
	j2.Delete("api.example.com", "", "sec")
	if len(j2.All()) != 1 {
		t.Errorf("after delete: %+v", j2.All())
	}
}
