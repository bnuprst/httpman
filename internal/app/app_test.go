package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
