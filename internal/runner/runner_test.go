package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bnuprst/httpman/internal/collection"
	"github.com/bnuprst/httpman/internal/cookies"
	"github.com/bnuprst/httpman/internal/httpclient"
	"github.com/bnuprst/httpman/internal/vars"
)

func testServer(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ User string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "s-" + body.User, Path: "/"})
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"token":"tok-%s"}`, body.User)
	})
	mux.HandleFunc("/users/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		ck, _ := r.Cookie("sid")
		sid := ""
		if ck != nil {
			sid = ck.Value
		}
		fmt.Fprintf(w, `{"path":%q,"auth":%q,"q":%q,"sid":%q,"key":%q}`, r.URL.Path, r.Header.Get("Authorization"), r.URL.RawQuery, sid, r.Header.Get("X-Api-Key"))
	})
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"ct": r.Header.Get("Content-Type"), "body": string(b), "method": r.Method})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

const fixture = `{
	"info": {"_postman_id": "c1", "name": "Demo", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
	"auth": {"type": "bearer", "bearer": [{"key": "token", "value": "{{token}}", "type": "string"}]},
	"variable": [{"key": "baseUrl", "value": "{{host}}"}],
	"event": [{"listen": "prerequest", "script": {"type": "text/javascript", "exec": ["pm.collectionVariables.set('calls', (pm.collectionVariables.get('calls') || 0) + 1);"]}}],
	"item": [
		{"name": "Login", "request": {"method": "POST", "auth": {"type": "noauth"},
			"header": [], "body": {"mode": "raw", "raw": "{\"user\":\"{{user}}\"}", "options": {"raw": {"language": "json"}}},
			"url": {"raw": "{{baseUrl}}/login", "host": ["{{baseUrl}}"], "path": ["login"]}},
		 "event": [{"listen": "test", "script": {"exec": ["pm.test('got token', () => pm.response.to.have.status(200));", "pm.environment.set('token', pm.response.json().token);"]}}]},
		{"name": "Users", "item": [
			{"name": "Get user", "request": {"method": "GET", "header": [],
				"url": {"raw": "{{baseUrl}}/users/:id?verbose=true", "host": ["{{baseUrl}}"], "path": ["users", ":id"],
					"query": [{"key": "verbose", "value": "true"}], "variable": [{"key": "id", "value": "{{userId}}"}]}},
			 "event": [{"listen": "test", "script": {"exec": [
				"const j = pm.response.json();",
				"pm.test('bearer inherited', () => pm.expect(j.auth).to.equal('Bearer tok-' + pm.iterationData.get('user')));",
				"pm.test('path var', () => pm.expect(j.path).to.equal('/users/' + data.userId));",
				"pm.test('cookie sent', () => pm.expect(j.sid).to.equal('s-' + data.user));",
				"pm.test('pm.cookies', () => pm.expect(pm.cookies.get('sid')).to.equal('s-' + data.user));"
			]}}]},
			{"name": "Api key folder", "auth": {"type": "apikey", "apikey": [{"key": "key", "value": "X-Api-Key"}, {"key": "value", "value": "secret"}]},
			 "item": [{"name": "Keyed", "request": {"method": "GET", "url": "{{baseUrl}}/users/k"},
				"event": [{"listen": "test", "script": {"exec": "pm.test('api key', () => pm.expect(pm.response.json().key).to.equal('secret'));\npostman.setNextRequest('Form');"}}]}]}
		]},
		{"name": "Skipped by setNextRequest", "request": {"method": "GET", "url": "{{baseUrl}}/users/never"},
		 "event": [{"listen": "test", "script": {"exec": ["pm.test('should not run', () => { throw new Error('ran'); });"]}}]},
		{"name": "Form", "request": {"method": "POST", "url": "{{baseUrl}}/echo",
			"body": {"mode": "urlencoded", "urlencoded": [{"key": "a", "value": "1 2"}, {"key": "b", "value": "{{user}}"}, {"key": "c", "value": "x", "disabled": true}]}},
		 "event": [{"listen": "test", "script": {"exec": [
			"const j = pm.response.json();",
			"pm.test('form body', () => { pm.expect(j.body).to.equal('a=1+2&b=' + data.user); pm.expect(j.ct).to.equal('application/x-www-form-urlencoded'); });",
			"pm.test('calls counted', () => pm.expect(pm.collectionVariables.get('calls')).to.be.above(2));"
		]}}]}
	]
}`

func TestCollectionRun(t *testing.T) {
	srv := testServer(t)
	c, err := collection.Parse([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}
	env := vars.NewScope("Local", []vars.Var{{Key: "host", Value: srv.URL, Enabled: true}})
	jar := cookies.New()
	opts := httpclient.DefaultOptions()
	opts.Jar = jar
	sess := NewSession(httpclient.New(opts), jar, c, nil, env)
	var logs []string
	sess.Console = func(e ConsoleEntry) { logs = append(logs, e.Level+": "+e.Message) }

	data, err := ParseData([]byte("user,userId\nada,1\nalan,2\n"), false)
	if err != nil {
		t.Fatal(err)
	}
	sum, err := sess.Run(context.Background(), RunOptions{Data: data}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Iterations != 2 {
		t.Errorf("iterations = %d", sum.Iterations)
	}
	// 4 requests per iteration (one skipped via setNextRequest).
	if sum.Requests != 8 {
		t.Errorf("requests = %d", sum.Requests)
	}
	for _, ex := range sum.Executions {
		if ex.Error != "" {
			t.Errorf("%s: %s", ex.Name, ex.Error)
		}
		for _, e := range ex.ScriptErrors {
			t.Errorf("%s: script error %s", ex.Name, e)
		}
		for _, tr := range ex.Tests {
			if !tr.Passed {
				body := ""
				if ex.Response != nil {
					body = ex.Response.BodyText
				}
				t.Errorf("iter %d %s: test %q failed: %+v (body %s)", ex.Iteration, ex.Name, tr.Name, tr.Error, body)
			}
		}
	}
	if sum.TestsPassed != 16 || sum.TestsFailed != 0 {
		t.Errorf("tests passed=%d failed=%d\n%s", sum.TestsPassed, sum.TestsFailed, strings.Join(logs, "\n"))
	}
	if v, _ := env.Get("token"); v != "tok-alan" {
		t.Errorf("env token = %v", v)
	}
	if v, _ := sess.CollectionVars.Get("calls"); v != float64(8) {
		t.Errorf("calls = %v", v)
	}
}

func TestSubstitutePathVars(t *testing.T) {
	got := substitutePathVars("http://h:8080/a/:id/b/:x?q=:id", map[string]string{"id": "7", "x": "y"})
	if got != "http://h:8080/a/7/b/y?q=:id" {
		t.Errorf("got %s", got)
	}
}

func TestParseV1(t *testing.T) {
	v1 := `{"id":"abc","name":"Old","order":["r1"],"folders":[{"id":"f1","name":"F","order":["r2"]}],
	"requests":[
	 {"id":"r1","name":"Root","url":"http://x/a","method":"get","headers":"A: 1\nB: 2","dataMode":"params","data":[{"key":"k","value":"v","type":"text"}],"tests":"tests['ok']=true;"},
	 {"id":"r2","name":"Nested","url":"http://x/b","method":"POST","dataMode":"raw","rawModeData":"{}","currentHelper":"basicAuth","helperAttributes":{"username":"u","password":"p"}}
	]}`
	c, err := collection.Parse([]byte(v1))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Item) != 2 || !c.Item[0].IsFolder() || c.Item[0].Item[0].Name != "Nested" || c.Item[1].Name != "Root" {
		b, _ := collection.Marshal(c)
		t.Fatalf("structure: %s", b)
	}
	root := c.Item[1].Request
	if root.Method != "GET" || len(root.Header) != 2 || root.Body.Mode != "formdata" || collection.ScriptFor(c.Item[1].Event, "test") == "" {
		t.Errorf("root: %+v", root)
	}
	if a := c.Item[0].Item[0].Request.Auth; a == nil || a.Type != "basic" || a.Get("username") != "u" {
		t.Errorf("auth: %+v", a)
	}
}
