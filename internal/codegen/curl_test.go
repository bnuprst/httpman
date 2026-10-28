package codegen

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bnuprst/httpman/internal/collection"
)

func TestParseCurl(t *testing.T) {
	r, err := ParseCurl(`curl -X PUT 'https://api.example.com/v1/items?id=5' \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer abc" \
  --data-raw '{"name":"it'\''s"}'`)
	if err != nil {
		t.Fatal(err)
	}
	if r.Method != "PUT" || r.URL.Raw != "https://api.example.com/v1/items?id=5" || len(r.Header) != 2 {
		t.Fatalf("%+v", r)
	}
	if r.Body.Mode != "raw" || r.Body.Raw != `{"name":"it's"}` || r.Body.RawLanguage() != "json" {
		t.Fatalf("body %+v", r.Body)
	}
	if len(r.URL.Query) != 1 || *r.URL.Query[0].Key != "id" {
		t.Fatalf("query %+v", r.URL.Query)
	}

	r, err = ParseCurl(`curl https://x.io/login -d user=ada -d "pass=a b" -u me:pw`)
	if err != nil {
		t.Fatal(err)
	}
	if r.Method != "POST" || r.Body.Mode != "urlencoded" || len(r.Body.URLEncoded) != 2 || r.Auth.Get("password") != "pw" {
		t.Fatalf("%+v %+v", r, r.Body)
	}

	r, err = ParseCurl(`curl.exe "https://x.io/up" -F "file=@C:\\tmp\\a.txt" -F name=x`)
	if err != nil {
		t.Fatal(err)
	}
	if r.Body.Mode != "formdata" || r.Body.FormData[0].Type != "file" || r.Body.FormData[0].Files()[0] != `C:\tmp\a.txt` {
		t.Fatalf("%+v", r.Body)
	}
}

func TestGenerate(t *testing.T) {
	r, _ := ParseCurl(`curl -X POST https://x.io/a -H 'X-A: 1' --data-raw '{"a":1}'`)
	s := &Snippet{Method: r.Method, URL: r.URL.Raw, Header: r.Header, Body: r.Body}
	for _, l := range Languages {
		out, err := Generate(s, l.ID)
		if err != nil || !strings.Contains(out, "x.io") {
			t.Errorf("%s: %v %s", l.ID, err, out)
		}
	}
	out, _ := Generate(s, "curl")
	back, err := ParseCurl(out)
	if err != nil || back.Body.Raw != `{"a":1}` || back.Method != "POST" {
		t.Errorf("round trip: %v %+v\n%s", err, back, out)
	}
}

func TestOAuth2SnippetMatchesSend(t *testing.T) {
	gen := func(params string) string {
		var a collection.Auth
		if err := json.Unmarshal([]byte(`{"type":"oauth2","oauth2":`+params+`}`), &a); err != nil {
			t.Fatal(err)
		}
		out, err := Generate(&Snippet{Method: "GET", URL: "https://x/y", Auth: &a}, "curl")
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if s := gen(`[{"key":"accessToken","value":"t"},{"key":"headerPrefix","value":null}]`); !strings.Contains(s, "Authorization: Bearer t") {
		t.Errorf("null prefix:\n%s", s)
	}
	if s := gen(`[{"key":"accessToken","value":"t"},{"key":"headerPrefix","value":""}]`); !strings.Contains(s, "Authorization: t'") {
		t.Errorf("empty prefix:\n%s", s)
	}
	if s := gen(`[{"key":"accessToken","value":"t"},{"key":"tokenType","value":"mac"}]`); strings.Contains(s, "Authorization") {
		t.Errorf("mac token:\n%s", s)
	}
	if s := gen(`[{"key":"accessToken","value":"t"},{"key":"addTokenTo","value":"queryParams"}]`); strings.Contains(s, "Authorization") || !strings.Contains(s, "https://x/y?access_token=t") {
		t.Errorf("query token:\n%s", s)
	}
}
