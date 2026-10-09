// Package runner executes collection items: scripts, variable resolution and
// HTTP, either one request at a time (GUI) or as a collection run (runner/CLI).
package runner

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/bnuprst/httpman/internal/collection"
	"github.com/bnuprst/httpman/internal/cookies"
	"github.com/bnuprst/httpman/internal/httpclient"
	"github.com/bnuprst/httpman/internal/script"
	"github.com/bnuprst/httpman/internal/vars"
)

// ConsoleEntry is a line in the console (script logs, requests, errors).
type ConsoleEntry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"` // log, info, warn, error, debug, request
	Message string    `json:"message"`
	Source  string    `json:"source,omitempty"`
	Data    any       `json:"data,omitempty"`
}

// Session holds the state shared by a sequence of executions.
type Session struct {
	Client         *httpclient.Client
	Jar            *cookies.Jar
	Collection     *collection.Collection // may be nil for ad-hoc requests
	Globals        *vars.Scope
	Environment    *vars.Scope // may be nil when no environment is selected
	CollectionVars *vars.Scope
	Local          *vars.Scope
	Console        func(ConsoleEntry)

	mu      sync.Mutex
	current string // name of the executing item, for console attribution
}

// NewSession creates a session; nil scopes are replaced by empty ones.
func NewSession(client *httpclient.Client, jar *cookies.Jar, c *collection.Collection, globals, env *vars.Scope) *Session {
	s := &Session{Client: client, Jar: jar, Collection: c, Globals: globals, Environment: env}
	if s.Globals == nil {
		s.Globals = vars.NewScope("globals", nil)
	}
	if c != nil {
		s.CollectionVars = vars.FromCollectionVars(c.Info.Name, c.Variable)
	} else {
		s.CollectionVars = vars.NewScope("collection", nil)
	}
	s.Local = vars.NewScope("_variables", nil)
	if s.Environment == nil {
		// Like newman, variables set via pm.environment without a selected
		// environment live for the duration of the session.
		s.Environment = vars.NewScope("", nil)
	}
	return s
}

func (s *Session) log(level, msg string, data any) {
	if s.Console == nil {
		return
	}
	s.mu.Lock()
	src := s.current
	s.mu.Unlock()
	s.Console(ConsoleEntry{Time: time.Now(), Level: level, Message: msg, Source: src, Data: data})
}

// Resolver returns the current variable resolver.
func (s *Session) Resolver(data *vars.Scope) vars.Resolver {
	scopes := []*vars.Scope{s.Globals, s.CollectionVars}
	if s.Environment != nil {
		scopes = append(scopes, s.Environment)
	}
	if data != nil {
		scopes = append(scopes, data)
	}
	scopes = append(scopes, s.Local)
	return vars.Resolver{Scopes: scopes}
}

// IterationInfo describes the position within a run.
type IterationInfo struct {
	Iteration      int
	IterationCount int
	Data           *vars.Scope
}

// ResponseView is a response as delivered to the UI.
type ResponseView struct {
	*httpclient.Response
	BodyBase64 string           `json:"bodyBase64"`
	BodyText   string           `json:"bodyText"`
	IsText     bool             `json:"isText"`
	Cookies    []cookies.Cookie `json:"cookies"`
}

// Execution is the result of running one request item.
type Execution struct {
	ItemID       string              `json:"itemId"`
	Name         string              `json:"name"`
	Iteration    int                 `json:"iteration"`
	Request      *httpclient.Request `json:"request,omitempty"`
	Response     *ResponseView       `json:"response,omitempty"`
	Error        string              `json:"error,omitempty"`
	Tests        []script.TestResult `json:"tests"`
	ScriptErrors []script.ErrorInfo  `json:"scriptErrors,omitempty"`
	NextRequest  *script.NextRequest `json:"nextRequest,omitempty"`
	Skipped      bool                `json:"skipped,omitempty"`
	Visualizer   json.RawMessage     `json:"visualizer,omitempty"`
	Path         []string            `json:"path,omitempty"`
}

// Failed reports whether the execution had an error or failing test.
func (e *Execution) Failed() bool {
	if e.Error != "" || len(e.ScriptErrors) > 0 {
		return true
	}
	for _, t := range e.Tests {
		if !t.Passed && !t.Skipped {
			return true
		}
	}
	return false
}

func scriptsFor(c *collection.Collection, parents []*collection.Item, item *collection.Item, listen string) []script.Source {
	var out []script.Source
	if c != nil {
		if code := collection.ScriptFor(c.Event, listen); code != "" {
			out = append(out, script.Source{Name: "collection " + c.Info.Name, Code: code})
		}
	}
	for _, p := range parents {
		if code := collection.ScriptFor(p.Event, listen); code != "" {
			out = append(out, script.Source{Name: "folder " + p.Name, Code: code})
		}
	}
	if code := collection.ScriptFor(item.Event, listen); code != "" {
		out = append(out, script.Source{Name: item.Name, Code: code})
	}
	return out
}

func (s *Session) scopesForScript() script.Scopes {
	sc := script.Scopes{
		Globals:             s.Globals.Vars,
		CollectionVariables: s.CollectionVars.Vars,
		Local:               s.Local.Vars,
	}
	if s.Environment != nil {
		sc.Environment = s.Environment.Vars
		sc.EnvironmentName = s.Environment.Name
	}
	return sc
}

func (s *Session) applyScopes(out *script.Output) {
	s.Globals.Vars = nonNil(out.Scopes.Globals)
	s.CollectionVars.Vars = nonNil(out.Scopes.CollectionVariables)
	if s.Environment != nil {
		s.Environment.Vars = nonNil(out.Scopes.Environment)
	}
	s.Local.Vars = nonNil(out.Scopes.Local)
}

func nonNil(v []vars.Var) []vars.Var {
	if v == nil {
		return []vars.Var{}
	}
	return v
}

// Execute runs one request item with its ancestors' scripts and auth.
func (s *Session) Execute(ctx context.Context, item *collection.Item, parents []*collection.Item, it IterationInfo) *Execution {
	ex := &Execution{ItemID: item.ID, Name: item.Name, Iteration: it.Iteration, Tests: []script.TestResult{}}
	for _, p := range parents {
		ex.Path = append(ex.Path, p.Name)
	}
	s.mu.Lock()
	s.current = item.Name
	s.mu.Unlock()
	if item.Request == nil {
		ex.Error = "item has no request"
		return ex
	}
	if it.IterationCount == 0 {
		it.IterationCount = 1
	}
	collName := ""
	if s.Collection != nil {
		collName = s.Collection.Info.Name
	}
	info := script.Info{Iteration: it.Iteration, IterationCount: it.IterationCount, RequestName: item.Name, RequestID: item.ID, Location: ex.Path}
	var dataVars []vars.Var
	if it.Data != nil {
		dataVars = it.Data.Vars
	}

	req := *item.Request
	req.Auth = EffectiveAuth(s.Collection, item, parents)
	host := &scriptHost{s: s, data: it.Data}

	// ---- pre-request scripts ----
	if pre := scriptsFor(s.Collection, parents, item, "prerequest"); len(pre) > 0 {
		reqJSON, _ := json.Marshal(req)
		info.EventName = "prerequest"
		out, err := script.Run(ctx, host, script.Input{
			Event: "prerequest", Info: info, CollectionName: collName,
			Scopes: withData(s.scopesForScript(), dataVars), Request: reqJSON,
		}, pre)
		if err != nil {
			ex.Error = err.Error()
			return ex
		}
		s.applyScopes(out)
		ex.Tests = append(ex.Tests, out.Tests...)
		ex.ScriptErrors = append(ex.ScriptErrors, out.Errors...)
		for _, e := range out.Errors {
			s.log("error", "Pre-request script error: "+e.String(), nil)
		}
		if out.NextRequest != nil {
			ex.NextRequest = out.NextRequest
		}
		var updated collection.Request
		if err := json.Unmarshal(out.Request, &updated); err == nil {
			req = updated
		}
		if out.SkipRequest {
			ex.Skipped = true
			return ex
		}
	}

	// ---- send ----
	resolved := Resolve(&req, s.Resolver(it.Data))
	ex.Request = resolved
	resp, err := s.Client.Do(ctx, resolved, ProtocolProfile(s.Collection, item, parents))
	if err != nil {
		ex.Error = err.Error()
		s.log("error", err.Error(), nil)
		return ex
	}
	ex.Response = s.view(resp)
	for _, w := range resp.Warnings {
		s.log("warn", w, nil)
	}
	s.log("request", fmt.Sprintf("%s %s → %d %s (%.0f ms)", resp.Request.Method, resp.Request.URL, resp.Code, resp.Status, resp.Time), map[string]any{
		"request":  resp.Request,
		"response": map[string]any{"code": resp.Code, "status": resp.Status, "header": resp.Header, "body": truncate(ex.Response.BodyText, 32<<10)},
	})

	// ---- test scripts ----
	if tests := scriptsFor(s.Collection, parents, item, "test"); len(tests) > 0 {
		sentReq := sentAsCollectionRequest(resolved, resp)
		sentReq.Description = req.Description
		reqJSON, _ := json.Marshal(sentReq)
		info.EventName = "test"
		out, err := script.Run(ctx, host, script.Input{
			Event: "test", Info: info, CollectionName: collName,
			Scopes: withData(s.scopesForScript(), dataVars), Request: reqJSON,
			Response: scriptResponse(resp, ex.Response.Cookies),
			Cookies:  ex.Response.Cookies,
		}, tests)
		if err != nil {
			ex.Error = err.Error()
			return ex
		}
		s.applyScopes(out)
		ex.Tests = append(ex.Tests, out.Tests...)
		ex.ScriptErrors = append(ex.ScriptErrors, out.Errors...)
		for _, e := range out.Errors {
			s.log("error", "Test script error: "+e.String(), nil)
		}
		if out.NextRequest != nil {
			ex.NextRequest = out.NextRequest
		}
		if len(out.Visualizer) > 0 {
			ex.Visualizer = out.Visualizer
		}
	}
	return ex
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func withData(sc script.Scopes, data []vars.Var) script.Scopes {
	sc.IterationData = data
	return sc
}

// sentAsCollectionRequest describes the request as it went over the wire
// (final URL, generated and auth headers), which is what test scripts see.
func sentAsCollectionRequest(r *httpclient.Request, resp *httpclient.Response) *collection.Request {
	out := &collection.Request{Method: r.Method, URL: collection.URL{Raw: r.URL}, Header: r.Header, Body: r.Body, Auth: r.Auth}
	if resp != nil {
		out.Method = resp.Request.Method
		out.URL = collection.URL{Raw: resp.Request.URL}
		out.Header = nil
		for _, h := range resp.Request.Header {
			if !strings.EqualFold(h.Key, "Cookie") {
				out.Header = append(out.Header, h)
			}
		}
	}
	return out
}

func (s *Session) view(resp *httpclient.Response) *ResponseView {
	v := &ResponseView{Response: resp, BodyBase64: base64.StdEncoding.EncodeToString(resp.Body), Cookies: []cookies.Cookie{}}
	v.IsText = utf8.Valid(resp.Body) && isTextual(resp)
	if v.IsText {
		v.BodyText = string(resp.Body)
	}
	if s.Jar != nil {
		if u, err := url.Parse(resp.Request.URL); err == nil {
			v.Cookies = nonNilCookies(s.Jar.Match(u))
		}
	}
	return v
}

func nonNilCookies(c []cookies.Cookie) []cookies.Cookie {
	if c == nil {
		return []cookies.Cookie{}
	}
	return c
}

func isTextual(resp *httpclient.Response) bool {
	ct := ""
	for _, h := range resp.Header {
		if strings.EqualFold(h.Key, "Content-Type") {
			ct = strings.ToLower(h.Value)
		}
	}
	if ct == "" {
		return true
	}
	for _, t := range []string{"image/", "audio/", "video/", "application/octet-stream", "application/pdf", "application/zip", "font/"} {
		if strings.HasPrefix(ct, t) {
			return false
		}
	}
	return true
}

func scriptResponse(resp *httpclient.Response, ck []cookies.Cookie) *script.ResponseData {
	return &script.ResponseData{
		Code: resp.Code, Status: resp.Status, Header: resp.Header, Body: string(resp.Body),
		ResponseTime: resp.Time, ResponseSize: resp.BodySize, Cookies: ck,
	}
}

// ---- script host ----

type scriptHost struct {
	s    *Session
	data *vars.Scope
}

func (h *scriptHost) Log(level, msg string) { h.s.log(level, msg, nil) }

func (h *scriptHost) Send(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var r collection.Request
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}
	// Variables were already substituted by the sandbox; resolve again with
	// an empty resolver only to normalize the structure.
	req := Resolve(&r, vars.Resolver{})
	resp, err := h.s.Client.Do(ctx, req, httpclient.RequestOverrides{})
	if err != nil {
		h.s.log("error", "pm.sendRequest: "+err.Error(), nil)
		return nil, err
	}
	h.s.log("request", fmt.Sprintf("%s %s → %d %s (%.0f ms) [pm.sendRequest]", resp.Request.Method, resp.Request.URL, resp.Code, resp.Status, resp.Time), map[string]any{"request": resp.Request})
	var ck []cookies.Cookie
	if h.s.Jar != nil {
		if u, err := url.Parse(resp.Request.URL); err == nil {
			ck = h.s.Jar.Match(u)
		}
	}
	return json.Marshal(scriptResponse(resp, ck))
}

func (h *scriptHost) Cookies(op string, raw json.RawMessage) (any, error) {
	if h.s.Jar == nil {
		return nil, errors.New("cookie jar disabled")
	}
	var a struct {
		URL    string         `json:"url"`
		Name   string         `json:"name"`
		Cookie cookies.Cookie `json:"cookie"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, err
	}
	u, err := httpclient.ParseURL(a.URL)
	if err != nil {
		return nil, err
	}
	switch op {
	case "get":
		for _, c := range h.s.Jar.Match(u) {
			if c.Name == a.Name {
				return c.Value, nil
			}
		}
		return nil, nil
	case "getAll":
		return nonNilCookies(h.s.Jar.Match(u)), nil
	case "set":
		c := a.Cookie
		if c.Domain == "" {
			c.Domain, c.HostOnly = u.Hostname(), true
		}
		h.s.Jar.Put(c)
		return c, nil
	case "unset":
		for _, c := range h.s.Jar.Match(u) {
			if c.Name == a.Name {
				h.s.Jar.Delete(c.Domain, c.Path, c.Name)
			}
		}
		return nil, nil
	case "clear":
		for _, c := range h.s.Jar.Match(u) {
			h.s.Jar.Delete(c.Domain, c.Path, c.Name)
		}
		return nil, nil
	}
	return nil, fmt.Errorf("unknown cookie operation %q", op)
}
