package script

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/bnuprst/httpman/internal/vars"
)

type testHost struct {
	mu   sync.Mutex
	logs []string
}

func (h *testHost) Log(level, msg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.logs = append(h.logs, level+": "+msg)
}

func (h *testHost) Send(_ context.Context, req json.RawMessage) (json.RawMessage, error) {
	var r struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(req, &r)
	body, _ := json.Marshal(map[string]string{"url": r.URL, "token": "abc123"})
	out, _ := json.Marshal(map[string]any{"code": 200, "status": "OK", "header": []map[string]string{{"key": "Content-Type", "value": "application/json"}}, "body": string(body)})
	return out, nil
}

func (h *testHost) Cookies(op string, args json.RawMessage) (any, error) { return nil, nil }

func jsonResponse(code int, body string) *ResponseData {
	return &ResponseData{
		Code: code, Status: "OK", Body: body, ResponseTime: 42, ResponseSize: int64(len(body)),
		Header: []map[string]string{{"key": "Content-Type", "value": "application/json; charset=utf-8"}, {"key": "X-Rate", "value": "10"}},
	}
}

func run(t *testing.T, in Input, code ...string) (*Output, *testHost) {
	t.Helper()
	h := &testHost{}
	var srcs []Source
	for i, c := range code {
		srcs = append(srcs, Source{Name: "script" + string(rune('0'+i)), Code: c})
	}
	if in.Request == nil {
		in.Request = json.RawMessage(`{"method":"GET","url":"https://example.com/api/users?page=1","header":[{"key":"Accept","value":"application/json"}]}`)
	}
	out, err := Run(context.Background(), h, in, srcs)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return out, h
}

func TestTestsAndAssertions(t *testing.T) {
	out, _ := run(t, Input{Event: "test", Response: jsonResponse(200, `{"id":7,"name":"Ada","tags":["a","b"],"nested":{"ok":true}}`)}, `
pm.test("status is 200", function () { pm.response.to.have.status(200); });
pm.test("is ok", () => pm.response.to.be.ok);
pm.test("not error", () => pm.response.to.not.be.error);
pm.test("json", () => { pm.response.to.be.json; pm.response.to.have.header("x-rate", "10"); });
pm.test("body", () => {
  const j = pm.response.json();
  pm.expect(j.name).to.eql("Ada");
  pm.expect(j.tags).to.have.lengthOf(2).and.include("b");
  pm.expect(j).to.have.nested.property("nested.ok", true);
});
pm.test("jsonBody", () => pm.response.to.have.jsonBody("nested.ok", true));
pm.test("schema", () => pm.response.to.have.jsonSchema({type: "object", required: ["id"], properties: {id: {type: "integer"}}}));
pm.test("fails", () => pm.expect(pm.response.code).to.equal(404));
pm.test.skip("skipped", () => {});
tests["legacy pass"] = responseCode.code === 200;
tests["legacy fail"] = JSON.parse(responseBody).id === 8;
`)
	want := map[string]bool{"status is 200": true, "is ok": true, "not error": true, "json": true, "body": true, "jsonBody": true, "schema": true, "fails": false, "skipped": true, "legacy pass": true, "legacy fail": false}
	if len(out.Errors) > 0 {
		t.Fatalf("errors: %+v", out.Errors)
	}
	got := map[string]TestResult{}
	for _, r := range out.Tests {
		got[r.Name] = r
	}
	for name, pass := range want {
		r, ok := got[name]
		if !ok {
			t.Errorf("missing test %q", name)
			continue
		}
		if r.Passed != pass {
			t.Errorf("test %q passed=%v want %v (err %+v)", name, r.Passed, pass, r.Error)
		}
	}
	if got["fails"].Error == nil || !strings.Contains(got["fails"].Error.Message, "expected 200 to equal 404") {
		t.Errorf("unexpected failure message: %+v", got["fails"].Error)
	}
	if !got["skipped"].Skipped {
		t.Errorf("skip not recorded")
	}
}

func TestVariablesAndRequestMutation(t *testing.T) {
	in := Input{
		Event: "prerequest",
		Scopes: Scopes{
			Globals:             []vars.Var{{Key: "g", Value: "global", Enabled: true}, {Key: "shadow", Value: "from-global", Enabled: true}},
			CollectionVariables: []vars.Var{{Key: "baseUrl", Value: "https://api.test", Enabled: true}},
			Environment:         []vars.Var{{Key: "shadow", Value: "from-env", Enabled: true}},
		},
	}
	out, h := run(t, in, `
pm.environment.set("token", "t-" + pm.globals.get("g"));
pm.collectionVariables.set("counter", 1);
pm.variables.set("local", "L");
postman.setGlobalVariable("legacy", "yes");
pm.globals.unset("g");
console.log("shadow is", pm.variables.get("shadow"), {a: 1});
console.log(pm.variables.replaceIn("{{baseUrl}}/x/{{local}}"));
pm.request.headers.add({key: "X-Token", value: pm.environment.get("token")});
pm.request.headers.upsert({key: "accept", value: "text/plain"});
pm.request.url.addQueryParams([{key: "q", value: "1"}]);
pm.request.url.query.remove("page");
pm.request.method = "POST";
pm.request.body = new (require("postman-collection").RequestBody)({mode: "raw", raw: "{\"a\":1}"});
`, `
// second script sees changes of the first
if (pm.environment.get("token") !== "t-global") throw new Error("not shared");
pm.execution.setNextRequest("Login");
`)
	if len(out.Errors) > 0 {
		t.Fatalf("errors: %+v", out.Errors)
	}
	get := func(list []vars.Var, k string) any {
		for _, v := range list {
			if v.Key == k {
				return v.Value
			}
		}
		return nil
	}
	if get(out.Scopes.Environment, "token") != "t-global" {
		t.Errorf("env token = %v", get(out.Scopes.Environment, "token"))
	}
	if get(out.Scopes.CollectionVariables, "counter") != float64(1) {
		t.Errorf("collection counter = %v", get(out.Scopes.CollectionVariables, "counter"))
	}
	if get(out.Scopes.Local, "local") != "L" || get(out.Scopes.Globals, "legacy") != "yes" || get(out.Scopes.Globals, "g") != nil {
		t.Errorf("scopes: %+v", out.Scopes)
	}
	if !strings.Contains(strings.Join(h.logs, "\n"), "log: shadow is from-env {\n  a: 1\n}") {
		t.Errorf("logs: %q", h.logs)
	}
	if !strings.Contains(strings.Join(h.logs, "\n"), "https://api.test/x/L") {
		t.Errorf("replaceIn logs: %q", h.logs)
	}
	var req struct {
		Method string `json:"method"`
		URL    struct {
			Raw string `json:"raw"`
		} `json:"url"`
		Header []struct{ Key, Value string } `json:"header"`
		Body   struct {
			Mode string `json:"mode"`
			Raw  string `json:"raw"`
		} `json:"body"`
	}
	if err := json.Unmarshal(out.Request, &req); err != nil {
		t.Fatal(err)
	}
	if req.Method != "POST" || req.URL.Raw != "https://example.com/api/users?q=1" || req.Body.Raw != `{"a":1}` {
		t.Errorf("request: %s", out.Request)
	}
	if len(req.Header) != 2 || req.Header[0].Value != "text/plain" || req.Header[1].Value != "t-global" {
		t.Errorf("headers: %+v", req.Header)
	}
	if out.NextRequest == nil || out.NextRequest.Value == nil || *out.NextRequest.Value != "Login" {
		t.Errorf("next request: %+v", out.NextRequest)
	}
}

func TestAsyncSendRequestAndTimers(t *testing.T) {
	out, h := run(t, Input{Event: "prerequest"}, `
pm.sendRequest("https://auth.test/token", function (err, res) {
  pm.environment.set("token", res.json().token);
  setTimeout(() => console.log("timer fired"), 5);
});
const res = await pm.sendRequest({url: "https://auth.test/other", method: "POST", header: {"X-A": "1"}});
pm.environment.set("other", res.json().url);
await new Promise(r => setTimeout(r, 10));
pm.test("async test", function (done) { setTimeout(() => { pm.expect(1).to.equal(1); done(); }, 1); });
`)
	if len(out.Errors) > 0 {
		t.Fatalf("errors: %+v", out.Errors)
	}
	vals := map[string]any{}
	for _, v := range out.Scopes.Environment {
		vals[v.Key] = v.Value
	}
	if vals["token"] != "abc123" || vals["other"] != "https://auth.test/other" {
		t.Errorf("env: %v", vals)
	}
	if !strings.Contains(strings.Join(h.logs, "\n"), "timer fired") {
		t.Errorf("logs: %v", h.logs)
	}
	if len(out.Tests) != 1 || !out.Tests[0].Passed {
		t.Errorf("tests: %+v", out.Tests)
	}
}

func TestErrorsAndReturn(t *testing.T) {
	out, _ := run(t, Input{Event: "test", Response: jsonResponse(500, "oops")}, `
if (pm.response.code === 500) { pm.test("early", () => {}); return; }
pm.test("never", () => {});
`, `undefinedFunction();`, `this is not valid js`)
	if len(out.Tests) != 1 || out.Tests[0].Name != "early" {
		t.Errorf("tests: %+v", out.Tests)
	}
	if len(out.Errors) != 2 {
		t.Fatalf("errors: %+v", out.Errors)
	}
	if out.Errors[0].Name != "ReferenceError" {
		t.Errorf("first error: %+v", out.Errors[0])
	}
	if out.Errors[1].Name != "SyntaxError" {
		t.Errorf("second error: %+v", out.Errors[1])
	}
}

func TestLibraries(t *testing.T) {
	out, h := run(t, Input{Event: "test", Response: jsonResponse(200, `<root><item id="1">a</item></root>`)}, `
const moment = require('moment');
const uuid = require('uuid');
const CryptoJS = require('crypto-js');
console.log(_.map([1,2], x => x * 2).join(","));
console.log(CryptoJS.SHA256("abc").toString());
console.log(CryptoJS.enc.Base64.stringify(CryptoJS.enc.Utf8.parse("hi")));
console.log(btoa("user:pass"), atob("aGk="));
console.log(moment.utc("2020-01-02").format("YYYY/MM/DD"));
console.log(/^[0-9a-f-]{36}$/.test(uuid.v4()), /^[0-9a-f-]{36}$/.test(uuid()));
console.log(JSON.stringify(xml2Json(pm.response.text())));
console.log(tv4.validate({a: 1}, {type: "object"}));
const $ = cheerio.load("<p class='x'>hello</p>");
console.log($("p.x").text());
console.log(require('csv-parse/lib/sync')("a,b\n1,2", {columns: true})[0].b);
console.log(Buffer.from("hey").toString("base64"));
console.log(pm.variables.replaceIn("{{$randomInt}}").length > 0);
`)
	if len(out.Errors) > 0 {
		t.Fatalf("errors: %+v\nlogs: %v", out.Errors, h.logs)
	}
	want := []string{
		"log: 2,4",
		"log: ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		"log: aGk=",
		"log: dXNlcjpwYXNz hi",
		"log: 2020/01/02",
		"log: true true",
		`log: {"root":{"item":{"_":"a","$":{"id":"1"}}}}`,
		"log: true",
		"log: hello",
		"log: 2",
		"log: aGV5",
		"log: true",
	}
	if strings.Join(h.logs, "\n") != strings.Join(want, "\n") {
		t.Errorf("logs:\n%s\nwant:\n%s", strings.Join(h.logs, "\n"), strings.Join(want, "\n"))
	}
}

func TestTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200_000_000)
	defer cancel()
	out, err := Run(ctx, &testHost{}, Input{Event: "prerequest", Request: json.RawMessage(`{"url":"http://x"}`)}, []Source{{Name: "loop", Code: "while(true){}"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Errors) != 1 || out.Errors[0].Name != "TimeoutError" {
		t.Errorf("errors: %+v", out.Errors)
	}
}

func TestLegacySandboxCompat(t *testing.T) {
	out, _ := run(t, Input{Event: "test", Response: jsonResponse(200, `{"args":{}}`)}, `
postman.setEnvironmentVariable("zero", 0);
postman.setEnvironmentVariable("one", 1);
tests["legacy setter keeps 0"] = postman.getEnvironmentVariable("zero") === 0;
tests["legacy setter stringifies"] = postman.getEnvironmentVariable("one") === "1";
tests["snapshot synced"] = environment.one === "1";
tests["sugar has"] = responseBody.has("args");
tests["sugar none"] = [1, 2].none(3);
pm.test("jsonSchema on values", () => {
  pm.expect({a: true}).to.be.jsonSchema({properties: {a: {type: "boolean"}}});
  pm.expect(() => pm.expect({a: 1}).to.be.jsonSchema({properties: {a: {type: "boolean"}}}))
    .to.throw("expected data to satisfy schema but found following errors: \ndata.a should be boolean");
});
pm.test("postman chai props", () => {
  pm.response.to.be.a.postmanResponse;
  pm.request.to.be.a.postmanRequest;
  pm.response.to.not.be.a.postmanRequest;
});
`)
	if len(out.Errors) > 0 {
		t.Fatalf("errors: %+v", out.Errors)
	}
	for _, r := range out.Tests {
		if !r.Passed {
			t.Errorf("%s failed: %+v", r.Name, r.Error)
		}
	}
	if len(out.Tests) != 7 {
		t.Errorf("tests: %d", len(out.Tests))
	}
}
