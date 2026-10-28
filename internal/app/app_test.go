package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/bnuprst/httpman/internal/collection"
	"github.com/bnuprst/httpman/internal/vars"
	"github.com/bnuprst/httpman/internal/workspace"
)

func TestImportSendPersist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "s", Value: "1"})
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"auth":%q}`, r.Header.Get("Authorization"))
	}))
	defer srv.Close()

	ws, err := workspace.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(ws, "test")

	coll := `{"info":{"_postman_id":"c1","name":"C","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json","x-custom":42},
	"auth":{"type":"basic","basic":[{"key":"username","value":"u"},{"key":"password","value":"{{pw}}"}]},
	"item":[{"id":"r1","name":"R","request":{"method":"GET","url":"{{host}}/x"},"x-item-extra":"keep",
	  "event":[{"listen":"test","script":{"exec":["pm.environment.set('seen', pm.response.json().auth);","pm.collectionVariables.set('n', 1);","pm.test('ok', () => pm.response.to.be.ok);"]}}]}]}`
	res, err := a.ImportText(coll)
	if err != nil || len(res.Collections) != 1 || len(res.Errors) != 0 {
		t.Fatalf("import: %v %+v", err, res)
	}
	env, err := a.SaveEnvironment(vars.Environment{Name: "E", Values: []vars.Var{{Key: "host", Value: srv.URL, Enabled: true}, {Key: "pw", Value: "p", Enabled: true}}})
	if err != nil {
		t.Fatal(err)
	}

	// The UI stores the JSON it received; unknown fields must survive a save.
	init, err := a.Init()
	if err != nil {
		t.Fatal(err)
	}
	raw := string(init.Collections[0])
	if _, err := a.SaveCollection(raw); err != nil {
		t.Fatal(err)
	}
	stored, _ := ws.CollectionRaw("c1")
	if !strings.Contains(string(stored), `"x-item-extra"`) {
		t.Errorf("unknown field lost:\n%s", stored)
	}

	var c struct {
		Item []json.RawMessage `json:"item"`
	}
	_ = json.Unmarshal(stored, &c)
	out, err := a.Send(SendInput{Collection: stored, ItemID: "r1", Item: c.Item[0], EnvironmentID: env.ID})
	if err != nil {
		t.Fatal(err)
	}
	ex := out.Execution
	if ex.Error != "" || len(ex.Tests) != 1 || !ex.Tests[0].Passed {
		t.Fatalf("execution: %+v", ex)
	}
	saved, _ := ws.Environment(env.ID)
	found := false
	for _, v := range saved.Values {
		if v.Key == "seen" && v.Value == "Basic dTpw" {
			found = true
		}
	}
	if !found {
		t.Errorf("environment not persisted: %+v", saved.Values)
	}
	if len(out.CollectionVariables) != 1 || out.CollectionVariables[0].Key != "n" {
		t.Errorf("collection variables: %+v", out.CollectionVariables)
	}
	if len(out.History) != 1 || out.History[0].Code != 200 {
		t.Errorf("history: %+v", out.History)
	}
	if cs := a.Cookies(); len(cs) != 1 || cs[0].Name != "s" {
		t.Errorf("cookies: %+v", cs)
	}

	// Code generation resolves inherited auth and variables.
	code, err := a.GenerateCode(SendInput{Collection: stored, ItemID: "r1", Item: c.Item[0], EnvironmentID: env.ID}, "curl")
	if err != nil || !strings.Contains(code, "Authorization: Basic dTpw") || !strings.Contains(code, srv.URL+"/x") {
		t.Errorf("code: %v\n%s", err, code)
	}
}

func TestImportCurlAndDump(t *testing.T) {
	ws, _ := workspace.Open(t.TempDir())
	a := New(ws, "test")
	res, _ := a.ImportText(`curl -X POST https://example.com/a -H "X: 1" -d '{"a":1}'`)
	if res.Request == nil || res.Request.Method != "POST" {
		t.Fatalf("%+v", res)
	}
	dump := `{"version":1,"collections":[{"id":"x","name":"Old","order":[],"requests":[]}],"environments":[{"id":"e","name":"Env","values":[{"key":"k","value":"v","enabled":true}]}]}`
	res, _ = a.ImportText(dump)
	if len(res.Collections) != 1 || len(res.Environments) != 1 || len(res.Errors) != 0 {
		t.Fatalf("%+v", res)
	}
}

func TestOAuth2TokenFromCollectionSecrets(t *testing.T) {
	var form, basic string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = r.PostForm.Encode()
		basic = r.Header.Get("Authorization")
		if r.URL.Path != "/oauth/token" {
			w.WriteHeader(404)
			return
		}
		fmt.Fprint(w, `{"access_token":"T","token_type":"Bearer"}`)
	}))
	defer srv.Close()

	ws, err := workspace.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(ws, "test")
	// As exported by Postman: collection-level secrets and OAuth 2.0 settings
	// referencing them.
	coll := `{"info":{"_postman_id":"c2","name":"C","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
	"auth":{"type":"oauth2","oauth2":[
	  {"key":"tokenRequestParams","value":[{"key":"audience","value":"{{aud}}","enabled":true,"send_as":"request_body"}],"type":"any"},
	  {"key":"grant_type","value":"client_credentials","type":"string"},
	  {"key":"accessTokenUrl","value":"{{baseUrl}}/oauth/token","type":"string"},
	  {"key":"clientId","value":"{{clientId}}","type":"string"},
	  {"key":"clientSecret","value":"{{clientSecret}}","type":"string"},
	  {"key":"tokenName","value":"svc","type":"string"},
	  {"key":"addTokenTo","value":"header","type":"string"}]},
	"variable":[{"key":"baseUrl","value":"` + srv.URL + `"},{"key":"clientId","value":"app"},
	  {"key":"clientSecret","value":"s3cret","type":"secret"},{"key":"aud","value":"api"}],
	"item":[]}`
	if res, err := a.ImportText(coll); err != nil || len(res.Errors) != 0 {
		t.Fatalf("import: %v %+v", err, res)
	}
	stored, _ := ws.CollectionRaw("c2")
	var c, orig struct {
		Auth     json.RawMessage `json:"auth"`
		Variable json.RawMessage `json:"variable"`
	}
	_ = json.Unmarshal(stored, &c)
	_ = json.Unmarshal([]byte(coll), &orig)
	// Import keeps Postman's OAuth 2.0 settings and secret variables as they are.
	same := func(x, y json.RawMessage) bool {
		var a, b any
		_ = json.Unmarshal(x, &a)
		_ = json.Unmarshal(y, &b)
		return reflect.DeepEqual(a, b)
	}
	if !same(c.Auth, orig.Auth) || !same(c.Variable, orig.Variable) {
		t.Errorf("import changed auth or variables:\n%s\n%s", c.Auth, c.Variable)
	}
	tok, err := a.GetOAuth2Token(OAuth2Input{RequestID: "r", Collection: stored, Auth: c.Auth})
	if err != nil || tok.AccessToken != "T" {
		t.Fatalf("token: %+v %v", tok, err)
	}
	if basic != "Basic YXBwOnMzY3JldA==" || form != "audience=api&grant_type=client_credentials" {
		t.Errorf("token request: auth=%q form=%q", basic, form)
	}
}

func TestImportExtractsOAuth2Variables(t *testing.T) {
	ws, _ := workspace.Open(t.TempDir())
	a := New(ws, "test")
	oauth := func(secret string) string {
		return `"auth":{"type":"oauth2","oauth2":[{"key":"clientId","value":"id1"},{"key":"clientSecret","value":"` + secret + `"},{"key":"accessTokenUrl","value":"{{tokenUrl}}"},{"key":"authUrl","value":"https://a"},{"key":"grant_type","value":"client_credentials"}]}`
	}
	// Environment variables win over collection variables, so their names are not used.
	if _, err := a.SaveEnvironment(vars.Environment{Name: "E", Values: []vars.Var{{Key: "clientSecret", Value: "env", Enabled: true}}}); err != nil {
		t.Fatal(err)
	}
	coll := `{"info":{"_postman_id":"o1","name":"O","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
	"variable":[{"key":"clientId","value":"other"},{"key":"authUrl","value":"https://a","disabled":true}],
	"item":[{"name":"F","item":[{"name":"R1","request":{"method":"GET","url":"x",` + oauth("s1") + `}}]},
	  {"name":"R2","request":{"method":"GET","url":"x",` + oauth("s1") + `}},
	  {"name":"R3","request":{"method":"GET","url":"x",` + oauth("s2") + `}}]}`
	res, err := a.ImportText(coll)
	if err != nil || len(res.Collections) != 1 {
		t.Fatalf("import: %v %+v", err, res)
	}
	c, err := collection.Parse(res.Collections[0])
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, v := range c.Variable {
		got[v.Key] = collection.ValueString(v.Value) + "/" + v.Type
	}
	want := map[string]string{"clientId": "other/", "clientId2": "id1/string", "clientSecret2": "s1/secret", "clientSecret3": "s2/secret", "authUrl": "https://a/", "authUrl2": "https://a/string"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("variables: %v", got)
	}
	r1 := c.Item[0].Item[0].Request.Auth
	r3 := c.Item[2].Request.Auth
	if r1.Get("clientId") != "{{clientId2}}" || r1.Get("clientSecret") != "{{clientSecret2}}" || r3.Get("clientSecret") != "{{clientSecret3}}" || r1.Get("authUrl") != "{{authUrl2}}" ||
		r1.Get("accessTokenUrl") != "{{tokenUrl}}" || r1.Get("grant_type") != "client_credentials" {
		t.Fatalf("auth: %+v / %+v", r1, r3)
	}
}
