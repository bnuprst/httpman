// Package app is the backend of the desktop UI. Its exported methods are
// bound to the frontend by Wails; it does not depend on Wails itself so it
// can be tested and reused.
package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/bnuprst/httpman/internal/codegen"
	"github.com/bnuprst/httpman/internal/collection"
	"github.com/bnuprst/httpman/internal/cookies"
	"github.com/bnuprst/httpman/internal/httpclient"
	"github.com/bnuprst/httpman/internal/runner"
	"github.com/bnuprst/httpman/internal/script"
	"github.com/bnuprst/httpman/internal/vars"
	"github.com/bnuprst/httpman/internal/workspace"
)

// FileFilter is a file dialog filter.
type FileFilter struct {
	DisplayName string
	Pattern     string // e.g. "*.json;*.txt"
}

// UI is implemented by the desktop shell (Wails) or by tests.
type UI interface {
	Emit(event string, data any)
	OpenFiles(title string, filters []FileFilter) ([]string, error)
	SaveFile(title, defaultName string, filters []FileFilter) (string, error)
}

// App is bound to the frontend.
type App struct {
	ws      *workspace.Workspace
	ui      UI
	version string

	mu       sync.Mutex
	settings workspace.Settings
	client   *httpclient.Client
	jar      *cookies.Jar
	cancels  map[string]context.CancelFunc
	saveJar  *time.Timer
}

// New creates the app backend.
func New(ws *workspace.Workspace, version string) *App {
	a := &App{ws: ws, version: version, cancels: map[string]context.CancelFunc{}, jar: cookies.New()}
	a.settings = ws.Settings()
	if a.settings.SaveCookies {
		_ = a.jar.Load(ws.CookiesPath())
	}
	a.jar.OnChange = a.scheduleJarSave
	a.rebuildClient()
	script.EnablePrewarm()
	return a
}

// SetUI attaches the UI shell.
func (a *App) SetUI(ui UI) { a.ui = ui }

func (a *App) emit(event string, data any) {
	if a.ui != nil {
		a.ui.Emit(event, data)
	}
}

func (a *App) scheduleJarSave() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.settings.SaveCookies {
		return
	}
	if a.saveJar != nil {
		a.saveJar.Stop()
	}
	a.saveJar = time.AfterFunc(500*time.Millisecond, func() { _ = a.jar.Save(a.ws.CookiesPath()) })
}

// Shutdown flushes state.
func (a *App) Shutdown() {
	a.mu.Lock()
	save := a.settings.SaveCookies
	a.mu.Unlock()
	if save {
		_ = a.jar.Save(a.ws.CookiesPath())
	}
}

func (a *App) rebuildClient() {
	s := a.settings
	opts := httpclient.DefaultOptions()
	opts.Timeout = time.Duration(s.TimeoutMs) * time.Millisecond
	opts.FollowRedirects = s.FollowRedirects
	opts.MaxRedirects = s.MaxRedirects
	opts.InsecureSkipVerify = !s.SSLVerify
	opts.ProxyMode = s.ProxyMode
	opts.ProxyURL = s.ProxyURL
	if s.MaxResponseMB > 0 {
		opts.MaxResponseBytes = int64(s.MaxResponseMB) << 20
	}
	opts.Jar = a.jar
	home, _ := os.UserHomeDir()
	opts.BaseDir = home
	if a.client != nil {
		a.client.Close()
	}
	a.client = httpclient.New(opts)
	if s.ScriptTimeoutSec > 0 {
		script.DefaultTimeout = time.Duration(s.ScriptTimeoutSec) * time.Second
	}
}

// ---- bootstrap ----

// InitData is everything the UI needs on startup.
type InitData struct {
	Version      string                   `json:"version"`
	Platform     string                   `json:"platform"`
	WorkspaceDir string                   `json:"workspaceDir"`
	Settings     workspace.Settings       `json:"settings"`
	Collections  []json.RawMessage        `json:"collections"`
	Environments []*vars.Environment      `json:"environments"`
	Globals      *vars.Environment        `json:"globals"`
	History      []workspace.HistoryEntry `json:"history"`
	UIState      json.RawMessage          `json:"uiState"`
	Errors       []string                 `json:"errors"`
	Languages    any                      `json:"languages"`
}

// Init loads the workspace.
func (a *App) Init() (*InitData, error) {
	d := &InitData{Version: a.version, Platform: runtime.GOOS, WorkspaceDir: a.ws.Dir, Settings: a.ws.Settings(), History: a.ws.History(), UIState: a.ws.UIState(), Collections: []json.RawMessage{}, Environments: []*vars.Environment{}, Errors: []string{}, Languages: codegen.Languages}
	metas, err := a.ws.Collections()
	if err != nil {
		return nil, err
	}
	for _, m := range metas {
		c, err := a.ws.Collection(m.ID)
		if err != nil {
			d.Errors = append(d.Errors, fmt.Sprintf("collection %s: %v", m.ID, err))
			continue
		}
		// Files edited outside httpman may lack item ids etc.; normalize
		// them (keeping unknown fields) before handing them to the UI.
		raw, _ := a.ws.CollectionRaw(m.ID)
		if needsNormalization(raw) {
			if _, err := a.ws.SaveCollectionRaw(raw); err == nil {
				raw, _ = a.ws.CollectionRaw(c.Info.PostmanID)
			}
		}
		d.Collections = append(d.Collections, raw)
	}
	envs, err := a.ws.Environments()
	if err != nil {
		return nil, err
	}
	for _, m := range envs {
		e, err := a.ws.Environment(m.ID)
		if err != nil {
			d.Errors = append(d.Errors, fmt.Sprintf("environment %s: %v", m.ID, err))
			continue
		}
		d.Environments = append(d.Environments, e)
	}
	if d.Globals, err = a.ws.Globals(); err != nil {
		return nil, err
	}
	return d, nil
}

// needsNormalization reports whether stored JSON lacks item ids or v2.1 schema.
func needsNormalization(raw []byte) bool {
	var probe struct {
		Info struct {
			Schema string `json:"schema"`
			ID     string `json:"_postman_id"`
		} `json:"info"`
		Item []json.RawMessage `json:"item"`
	}
	if json.Unmarshal(raw, &probe) != nil || probe.Info.Schema != collection.SchemaV21 || probe.Info.ID == "" {
		return true
	}
	var check func(items []json.RawMessage) bool
	check = func(items []json.RawMessage) bool {
		for _, it := range items {
			var p struct {
				ID      string            `json:"id"`
				Item    []json.RawMessage `json:"item"`
				Request json.RawMessage   `json:"request"`
			}
			if json.Unmarshal(it, &p) != nil || p.ID == "" {
				return true
			}
			var r struct {
				URL json.RawMessage `json:"url"`
			}
			if len(p.Request) > 0 && (p.Request[0] == '"' || json.Unmarshal(p.Request, &r) != nil || len(r.URL) > 0 && r.URL[0] == '"') {
				return true
			}
			if check(p.Item) {
				return true
			}
		}
		return false
	}
	return check(probe.Item)
}

// ---- collections ----

// SaveCollection stores collection JSON and returns its id.
func (a *App) SaveCollection(data string) (string, error) {
	return a.ws.SaveCollectionRaw([]byte(data))
}

// DeleteCollection deletes a collection.
func (a *App) DeleteCollection(id string) error { return a.ws.DeleteCollection(id) }

// NewCollection creates and stores an empty collection.
func (a *App) NewCollection(name string) (json.RawMessage, error) {
	c := &collection.Collection{Info: collection.Info{Name: name}}
	collection.Normalize(c)
	raw, err := collection.Marshal(c)
	if err != nil {
		return nil, err
	}
	if _, err := a.ws.SaveCollectionRaw(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// ImportResult lists what an import produced.
type ImportResult struct {
	Collections  []json.RawMessage   `json:"collections"`
	Environments []*vars.Environment `json:"environments"`
	Globals      *vars.Environment   `json:"globals,omitempty"`
	Request      *collection.Request `json:"request,omitempty"` // from cURL
	Errors       []string            `json:"errors"`
}

func (a *App) importData(name string, data []byte, res *ImportResult) {
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "curl") {
		req, err := codegen.ParseCurl(trimmed)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", name, err))
			return
		}
		res.Request = req
		return
	}
	// Postman data dump: {"version":1,"collections":[...],"environments":[...]}
	var dump struct {
		Collections  []json.RawMessage `json:"collections"`
		Environments []json.RawMessage `json:"environments"`
		Globals      json.RawMessage   `json:"globals"`
	}
	if json.Unmarshal(data, &dump) == nil && (len(dump.Collections) > 0 || len(dump.Environments) > 0) {
		for _, c := range dump.Collections {
			a.importData(name, c, res)
		}
		for _, e := range dump.Environments {
			a.importData(name, e, res)
		}
		return
	}
	if _, raw, err := a.ws.ImportCollection(data); err == nil {
		res.Collections = append(res.Collections, raw)
		return
	}
	var probe struct {
		Scope string `json:"_postman_variable_scope"`
	}
	_ = json.Unmarshal(data, &probe)
	if probe.Scope == "globals" {
		g, err := vars.ParseEnvironment(data)
		if err == nil {
			cur, _ := a.ws.Globals()
			for _, v := range g.Values {
				s := vars.Scope{Vars: cur.Values}
				s.Set(v.Key, v.Value)
				cur.Values = s.Vars
			}
			if err := a.ws.SaveGlobals(cur); err == nil {
				res.Globals = cur
				return
			}
		}
	}
	if e, err := a.ws.ImportEnvironment(data); err == nil {
		res.Environments = append(res.Environments, e)
		return
	}
	res.Errors = append(res.Errors, fmt.Sprintf("%s: not a Postman collection, environment, globals file or cURL command", name))
}

// ImportFiles opens a file dialog and imports the selected files.
func (a *App) ImportFiles() (*ImportResult, error) {
	res := &ImportResult{Collections: []json.RawMessage{}, Environments: []*vars.Environment{}, Errors: []string{}}
	if a.ui == nil {
		return res, nil
	}
	paths, err := a.ui.OpenFiles("Import", []FileFilter{{"Postman files (*.json)", "*.json"}, {"All files", "*.*"}})
	if err != nil {
		return nil, err
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			res.Errors = append(res.Errors, err.Error())
			continue
		}
		a.importData(filepath.Base(p), data, res)
	}
	return res, nil
}

// ImportText imports pasted JSON or a cURL command.
func (a *App) ImportText(text string) (*ImportResult, error) {
	res := &ImportResult{Collections: []json.RawMessage{}, Environments: []*vars.Environment{}, Errors: []string{}}
	a.importData("pasted text", []byte(text), res)
	return res, nil
}

// ExportCollection writes a collection to a user-chosen file.
func (a *App) ExportCollection(data string) (string, error) {
	c, err := collection.Parse([]byte(data))
	if err != nil {
		return "", err
	}
	if a.ui == nil {
		return "", errors.New("no UI")
	}
	path, err := a.ui.SaveFile("Export collection", sanitizeFilename(c.Info.Name)+".postman_collection.json", []FileFilter{{"Postman collection", "*.json"}})
	if err != nil || path == "" {
		return "", err
	}
	var buf json.RawMessage = []byte(data)
	out, err := json.MarshalIndent(buf, "", "\t")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, out, 0o644)
}

func sanitizeFilename(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '_'
		}
		return r
	}, s)
	if s == "" {
		return "export"
	}
	return s
}

// ---- environments & globals ----

// SaveEnvironment stores an environment (creating it when id is empty).
func (a *App) SaveEnvironment(env vars.Environment) (*vars.Environment, error) {
	e := env
	if err := a.ws.SaveEnvironment(&e); err != nil {
		return nil, err
	}
	return &e, nil
}

// DeleteEnvironment deletes an environment.
func (a *App) DeleteEnvironment(id string) error { return a.ws.DeleteEnvironment(id) }

// SaveGlobals stores globals.
func (a *App) SaveGlobals(g vars.Environment) (*vars.Environment, error) {
	if err := a.ws.SaveGlobals(&g); err != nil {
		return nil, err
	}
	return &g, nil
}

// ExportEnvironment writes an environment to a user-chosen file.
func (a *App) ExportEnvironment(id string) (string, error) {
	var env *vars.Environment
	var err error
	if id == "globals" {
		env, err = a.ws.Globals()
	} else {
		env, err = a.ws.Environment(id)
	}
	if err != nil {
		return "", err
	}
	if a.ui == nil {
		return "", errors.New("no UI")
	}
	suffix := ".postman_environment.json"
	if id == "globals" {
		suffix = ".postman_globals.json"
	}
	path, err := a.ui.SaveFile("Export", sanitizeFilename(env.Name)+suffix, []FileFilter{{"Postman environment", "*.json"}})
	if err != nil || path == "" {
		return "", err
	}
	out, _ := json.MarshalIndent(env, "", "\t")
	return path, os.WriteFile(path, out, 0o644)
}

// ---- settings, history, state ----

// SaveSettings stores settings and applies them.
func (a *App) SaveSettings(s workspace.Settings) error {
	if err := a.ws.SaveSettings(s); err != nil {
		return err
	}
	a.mu.Lock()
	a.settings = s
	a.rebuildClient()
	a.mu.Unlock()
	return nil
}

// History returns history entries.
func (a *App) History() []workspace.HistoryEntry { return a.ws.History() }

// DeleteHistory removes an entry (or all when id is empty).
func (a *App) DeleteHistory(id string) ([]workspace.HistoryEntry, error) {
	if err := a.ws.DeleteHistory(id); err != nil {
		return nil, err
	}
	return a.ws.History(), nil
}

// SaveUIState persists frontend state.
func (a *App) SaveUIState(state string) error {
	if !json.Valid([]byte(state)) {
		return errors.New("invalid state JSON")
	}
	return a.ws.SaveUIState(json.RawMessage(state))
}

// ---- cookies ----

// Cookies lists all cookies.
func (a *App) Cookies() []cookies.Cookie { return a.jar.All() }

// PutCookie adds or replaces a cookie.
func (a *App) PutCookie(c cookies.Cookie) []cookies.Cookie {
	a.jar.Put(c)
	return a.jar.All()
}

// DeleteCookies deletes cookies; empty fields act as wildcards.
func (a *App) DeleteCookies(domain, path, name string) []cookies.Cookie {
	a.jar.Delete(domain, path, name)
	return a.jar.All()
}

// ---- files ----

// PickFiles lets the user choose files (form-data / binary bodies, data files).
func (a *App) PickFiles(title string) ([]string, error) {
	if a.ui == nil {
		return nil, nil
	}
	return a.ui.OpenFiles(title, nil)
}

// ReadTextFile returns a file's content (data file previews).
func (a *App) ReadTextFile(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if st.Size() > 50<<20 {
		return "", errors.New("file too large")
	}
	b, err := os.ReadFile(path)
	return string(b), err
}

// SaveResponse writes a response body (base64) to a user-chosen file.
func (a *App) SaveResponse(bodyBase64, defaultName string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(bodyBase64)
	if err != nil {
		return "", err
	}
	if a.ui == nil {
		return "", errors.New("no UI")
	}
	path, err := a.ui.SaveFile("Save response", defaultName, nil)
	if err != nil || path == "" {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o644)
}

// ---- sending ----

// SendInput describes a request to send from the UI.
type SendInput struct {
	RequestID     string          `json:"requestId"`     // used for cancellation
	Collection    json.RawMessage `json:"collection"`    // in-memory collection (optional)
	ItemID        string          `json:"itemId"`        // item within the collection (optional)
	Item          json.RawMessage `json:"item"`          // the (possibly unsaved) item being sent
	EnvironmentID string          `json:"environmentId"` // may be empty
}

// SendResult is returned to the UI.
type SendResult struct {
	Execution           *runner.Execution        `json:"execution"`
	Environment         *vars.Environment        `json:"environment,omitempty"`
	Globals             *vars.Environment        `json:"globals"`
	CollectionVariables []vars.Var               `json:"collectionVariables,omitempty"`
	History             []workspace.HistoryEntry `json:"history"`
}

func (a *App) prepare(in SendInput) (*runner.Session, *collection.Item, []*collection.Item, *vars.Environment, *vars.Environment, error) {
	var item collection.Item
	if err := json.Unmarshal(in.Item, &item); err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("invalid request: %w", err)
	}
	if item.Request == nil {
		return nil, nil, nil, nil, nil, errors.New("item has no request")
	}
	var coll *collection.Collection
	var parents []*collection.Item
	if len(in.Collection) > 0 && string(in.Collection) != "null" {
		c, err := collection.Parse(in.Collection)
		if err != nil {
			return nil, nil, nil, nil, nil, err
		}
		coll = c
		if in.ItemID != "" {
			if found, p := c.Find(in.ItemID); found != nil && found.ID == in.ItemID {
				parents = p
			}
		}
	}
	globals, err := a.ws.Globals()
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	var env *vars.Environment
	var envScope *vars.Scope
	if in.EnvironmentID != "" {
		env, err = a.ws.Environment(in.EnvironmentID)
		if err != nil {
			return nil, nil, nil, nil, nil, fmt.Errorf("environment: %w", err)
		}
		envScope = vars.NewScope(env.Name, env.Values)
	}
	a.mu.Lock()
	client := a.client
	a.mu.Unlock()
	sess := runner.NewSession(client, a.jar, coll, vars.NewScope("globals", globals.Values), envScope)
	sess.Console = func(e runner.ConsoleEntry) { a.emit("console", e) }
	return sess, &item, parents, env, globals, nil
}

// Send executes a request with its scripts.
func (a *App) Send(in SendInput) (*SendResult, error) {
	sess, item, parents, env, globals, err := a.prepare(in)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	if in.RequestID != "" {
		a.mu.Lock()
		a.cancels[in.RequestID] = cancel
		a.mu.Unlock()
	}
	defer func() {
		cancel()
		a.mu.Lock()
		delete(a.cancels, in.RequestID)
		a.mu.Unlock()
	}()
	ex := sess.Execute(ctx, item, parents, runner.IterationInfo{})
	if ctx.Err() != nil && ex.Error != "" {
		ex.Error = "Request cancelled"
	}

	res := &SendResult{Execution: ex}
	globals.Values = sess.Globals.Vars
	res.Globals = globals
	a.mu.Lock()
	persist := a.settings.PersistVariables
	limit := a.settings.HistoryLimit
	a.mu.Unlock()
	if persist {
		if err := a.ws.SaveGlobals(globals); err != nil {
			return nil, err
		}
	}
	if env != nil {
		env.Values = sess.Environment.Vars
		res.Environment = env
		if persist {
			if err := a.ws.SaveEnvironment(env); err != nil {
				return nil, err
			}
		}
	}
	if sess.Collection != nil {
		res.CollectionVariables = sess.CollectionVars.Vars
	}

	h := workspace.HistoryEntry{Name: item.Name, Method: item.Request.Method, URL: item.Request.URL.String(), Request: mustJSON(item.Request)}
	if ex.Request != nil {
		h.Method = ex.Request.Method
	}
	if ex.Response != nil {
		h.Code = ex.Response.Code
		h.Duration = ex.Response.Time
	}
	if err := a.ws.AddHistory(h, limit); err == nil {
		res.History = a.ws.History()
	}
	return res, nil
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// Cancel aborts an in-flight request or run.
func (a *App) Cancel(requestID string) {
	a.mu.Lock()
	cancel := a.cancels[requestID]
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// ResolveRequest substitutes variables (no scripts) for code generation.
func (a *App) ResolveRequest(in SendInput) (*httpclient.Request, error) {
	sess, item, parents, _, _, err := a.prepare(in)
	if err != nil {
		return nil, err
	}
	req := *item.Request
	req.Auth = runner.EffectiveAuth(sess.Collection, item, parents)
	return runner.Resolve(&req, sess.Resolver(nil)), nil
}

// GenerateCode renders a code snippet for a request.
func (a *App) GenerateCode(in SendInput, lang string) (string, error) {
	r, err := a.ResolveRequest(in)
	if err != nil {
		return "", err
	}
	return codegen.Generate(&codegen.Snippet{Method: r.Method, URL: r.URL, Header: r.Header, Body: r.Body, Auth: r.Auth}, lang)
}

// ---- collection runner ----

// RunInput configures a collection run from the UI.
type RunInput struct {
	RunID         string          `json:"runId"`
	Collection    json.RawMessage `json:"collection"`
	FolderID      string          `json:"folderId"`
	EnvironmentID string          `json:"environmentId"`
	Iterations    int             `json:"iterations"`
	DelayMs       int             `json:"delayMs"`
	DataFile      string          `json:"dataFile"`
	PersistVars   bool            `json:"persistVariables"`
	Bail          bool            `json:"bail"`
	Only          []string        `json:"only"`
}

// StartRun starts a collection run in the background. Progress is emitted
// as "run:<runId>" events; the final event has type "done" (or "error").
func (a *App) StartRun(in RunInput) (string, error) {
	c, err := collection.Parse(in.Collection)
	if err != nil {
		return "", err
	}
	if in.RunID == "" {
		in.RunID = uuid.NewString()
	}
	var data *runner.DataRows
	if in.DataFile != "" {
		if data, err = runner.LoadDataFile(in.DataFile); err != nil {
			return "", fmt.Errorf("data file: %w", err)
		}
	}
	globals, err := a.ws.Globals()
	if err != nil {
		return "", err
	}
	var env *vars.Environment
	var envScope *vars.Scope
	if in.EnvironmentID != "" {
		if env, err = a.ws.Environment(in.EnvironmentID); err != nil {
			return "", err
		}
		envScope = vars.NewScope(env.Name, env.Values)
	}
	a.mu.Lock()
	client := a.client
	a.mu.Unlock()
	sess := runner.NewSession(client, a.jar, c, vars.NewScope("globals", globals.Values), envScope)
	event := "run:" + in.RunID
	sess.Console = func(e runner.ConsoleEntry) { a.emit("console", e) }

	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.cancels[in.RunID] = cancel
	a.mu.Unlock()
	go func() {
		defer func() {
			cancel()
			a.mu.Lock()
			delete(a.cancels, in.RunID)
			a.mu.Unlock()
		}()
		sum, err := sess.Run(ctx, runner.RunOptions{FolderID: in.FolderID, Iterations: in.Iterations, Data: data, Delay: time.Duration(in.DelayMs) * time.Millisecond, Bail: in.Bail, Only: in.Only}, func(ev runner.RunEvent) {
			if ev.Type == "done" {
				return // emitted below, after variables are persisted
			}
			a.emit(event, ev)
		})
		if err != nil {
			a.emit(event, map[string]any{"type": "error", "error": err.Error()})
			return
		}
		done := map[string]any{"type": "done", "summary": sum, "collectionVariables": sess.CollectionVars.Vars}
		if in.PersistVars {
			globals.Values = sess.Globals.Vars
			_ = a.ws.SaveGlobals(globals)
			done["globals"] = globals
			if env != nil {
				env.Values = sess.Environment.Vars
				_ = a.ws.SaveEnvironment(env)
				done["environment"] = env
			}
		}
		a.emit(event, done)
	}()
	return in.RunID, nil
}

// LoadDataFile parses a runner data file for preview.
func (a *App) LoadDataFile(path string) (*runner.DataRows, error) { return runner.LoadDataFile(path) }

// DynamicVariables lists supported {{$dynamic}} variables.
func (a *App) DynamicVariables() []string { return vars.DynamicNames() }

// ImportPaths imports dropped files.
func (a *App) ImportPaths(paths []string) *ImportResult {
	res := &ImportResult{Collections: []json.RawMessage{}, Environments: []*vars.Environment{}, Errors: []string{}}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			res.Errors = append(res.Errors, err.Error())
			continue
		}
		a.importData(filepath.Base(p), data, res)
	}
	return res
}
