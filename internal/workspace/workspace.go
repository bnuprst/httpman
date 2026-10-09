// Package workspace stores collections, environments, globals, history,
// cookies and settings as plain files in a local directory. Collections and
// environments are stored in Postman's own export formats, so the directory
// can be versioned with git or shared as-is.
package workspace

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/bnuprst/httpman/internal/collection"
	"github.com/bnuprst/httpman/internal/vars"
)

const (
	collectionSuffix  = ".postman_collection.json"
	environmentSuffix = ".postman_environment.json"
	globalsFile       = "globals.postman_globals.json"
	settingsFile      = "settings.json"
	historyFile       = "history.json"
	cookiesFile       = "cookies.json"
	stateFile         = "state.json"
)

// DefaultDir returns $HTTPMAN_HOME or <user config dir>/httpman.
func DefaultDir() string {
	if d := os.Getenv("HTTPMAN_HOME"); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".httpman")
	}
	return filepath.Join(base, "httpman")
}

// Workspace is a directory of httpman data.
type Workspace struct {
	Dir string
	mu  sync.Mutex
}

// Open creates (if needed) and opens a workspace directory.
func Open(dir string) (*Workspace, error) {
	for _, d := range []string{dir, filepath.Join(dir, "collections"), filepath.Join(dir, "environments")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	return &Workspace{Dir: dir}, nil
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

func safeID(id string) error {
	if !idPattern.MatchString(id) || strings.Contains(id, "..") {
		return fmt.Errorf("invalid id %q", id)
	}
	return nil
}

// writeFile writes atomically via a temp file and rename.
func writeFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

func pretty(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "\t")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ---- collections ----

// Meta identifies a stored collection or environment.
type Meta struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (w *Workspace) collectionPath(id string) string {
	return filepath.Join(w.Dir, "collections", id+collectionSuffix)
}

// Collections lists stored collections sorted by name.
func (w *Workspace) Collections() ([]Meta, error) {
	entries, err := os.ReadDir(filepath.Join(w.Dir, "collections"))
	if err != nil {
		return nil, err
	}
	out := []Meta{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), collectionSuffix) {
			continue
		}
		id := strings.TrimSuffix(e.Name(), collectionSuffix)
		data, err := os.ReadFile(filepath.Join(w.Dir, "collections", e.Name()))
		if err != nil {
			continue
		}
		var probe struct {
			Info struct {
				Name string `json:"name"`
			} `json:"info"`
		}
		_ = json.Unmarshal(data, &probe)
		out = append(out, Meta{ID: id, Name: probe.Info.Name})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// CollectionRaw returns the stored JSON of a collection.
func (w *Workspace) CollectionRaw(id string) ([]byte, error) {
	if err := safeID(id); err != nil {
		return nil, err
	}
	return os.ReadFile(w.collectionPath(id))
}

// Collection loads and parses a stored collection.
func (w *Workspace) Collection(id string) (*collection.Collection, error) {
	data, err := w.CollectionRaw(id)
	if err != nil {
		return nil, err
	}
	return collection.Parse(data)
}

// SaveCollectionRaw validates and stores collection JSON under its _postman_id.
// Unknown fields are preserved.
func (w *Workspace) SaveCollectionRaw(data []byte) (string, error) {
	out, c, err := collection.NormalizeRaw(data)
	if err != nil {
		return "", err
	}
	id := c.Info.PostmanID
	if err := safeID(id); err != nil {
		return "", err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return id, writeFile(w.collectionPath(id), out)
}

// ImportCollection parses any supported collection format, normalizes it to
// v2.1 (keeping unknown fields) and stores it. A colliding id is replaced.
func (w *Workspace) ImportCollection(data []byte) (*collection.Collection, []byte, error) {
	out, c, err := collection.NormalizeRaw(data)
	if err != nil {
		return nil, nil, err
	}
	if safeID(c.Info.PostmanID) != nil || w.exists(c.Info.PostmanID) {
		var root map[string]any
		dec := json.NewDecoder(bytes.NewReader(out))
		dec.UseNumber()
		if err := dec.Decode(&root); err != nil {
			return nil, nil, err
		}
		root["info"].(map[string]any)["_postman_id"] = uuid.NewString()
		if out, err = pretty(root); err != nil {
			return nil, nil, err
		}
		if c, err = collection.Parse(out); err != nil {
			return nil, nil, err
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return c, out, writeFile(w.collectionPath(c.Info.PostmanID), out)
}

func (w *Workspace) exists(id string) bool {
	_, err := os.Stat(w.collectionPath(id))
	return err == nil
}

// DeleteCollection removes a collection file.
func (w *Workspace) DeleteCollection(id string) error {
	if err := safeID(id); err != nil {
		return err
	}
	return os.Remove(w.collectionPath(id))
}

// ---- environments ----

func (w *Workspace) envPath(id string) string {
	return filepath.Join(w.Dir, "environments", id+environmentSuffix)
}

// Environments lists stored environments sorted by name.
func (w *Workspace) Environments() ([]Meta, error) {
	entries, err := os.ReadDir(filepath.Join(w.Dir, "environments"))
	if err != nil {
		return nil, err
	}
	out := []Meta{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), environmentSuffix) {
			continue
		}
		id := strings.TrimSuffix(e.Name(), environmentSuffix)
		env, err := w.Environment(id)
		if err != nil {
			continue
		}
		out = append(out, Meta{ID: id, Name: env.Name})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// Environment loads an environment.
func (w *Workspace) Environment(id string) (*vars.Environment, error) {
	if err := safeID(id); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(w.envPath(id))
	if err != nil {
		return nil, err
	}
	env, err := vars.ParseEnvironment(data)
	if err != nil {
		return nil, err
	}
	env.ID = id
	return env, nil
}

// SaveEnvironment stores an environment, assigning an id if needed.
func (w *Workspace) SaveEnvironment(env *vars.Environment) error {
	if env.ID == "" {
		env.ID = uuid.NewString()
	}
	if err := safeID(env.ID); err != nil {
		return err
	}
	if env.Values == nil {
		env.Values = []vars.Var{}
	}
	env.Scope = "environment"
	env.ExportedAt = time.Now().UTC().Format(time.RFC3339)
	env.ExportedUsing = "httpman"
	data, err := pretty(env)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return writeFile(w.envPath(env.ID), data)
}

// ImportEnvironment stores a Postman environment export.
func (w *Workspace) ImportEnvironment(data []byte) (*vars.Environment, error) {
	env, err := vars.ParseEnvironment(data)
	if err != nil {
		return nil, err
	}
	if env.ID == "" || safeID(env.ID) != nil {
		env.ID = uuid.NewString()
	} else if _, err := os.Stat(w.envPath(env.ID)); err == nil {
		env.ID = uuid.NewString()
	}
	return env, w.SaveEnvironment(env)
}

// DeleteEnvironment removes an environment file.
func (w *Workspace) DeleteEnvironment(id string) error {
	if err := safeID(id); err != nil {
		return err
	}
	return os.Remove(w.envPath(id))
}

// ---- globals ----

// Globals loads the global variables.
func (w *Workspace) Globals() (*vars.Environment, error) {
	data, err := os.ReadFile(filepath.Join(w.Dir, globalsFile))
	if errors.Is(err, os.ErrNotExist) {
		return &vars.Environment{ID: "globals", Name: "Globals", Values: []vars.Var{}, Scope: "globals"}, nil
	}
	if err != nil {
		return nil, err
	}
	g, err := vars.ParseEnvironment(data)
	if err != nil {
		return nil, err
	}
	if g.Name == "" {
		g.Name = "Globals"
	}
	return g, nil
}

// SaveGlobals stores the global variables.
func (w *Workspace) SaveGlobals(g *vars.Environment) error {
	g.Scope = "globals"
	if g.ID == "" {
		g.ID = "globals"
	}
	if g.Name == "" {
		g.Name = "Globals"
	}
	if g.Values == nil {
		g.Values = []vars.Var{}
	}
	g.ExportedAt = time.Now().UTC().Format(time.RFC3339)
	g.ExportedUsing = "httpman"
	data, err := pretty(g)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return writeFile(filepath.Join(w.Dir, globalsFile), data)
}

// ---- generic JSON documents (settings, history, UI state) ----

func (w *Workspace) loadJSON(name string, v any) error {
	data, err := os.ReadFile(filepath.Join(w.Dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func (w *Workspace) saveJSON(name string, v any) error {
	data, err := pretty(v)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return writeFile(filepath.Join(w.Dir, name), data)
}

// CookiesPath is where the cookie jar is persisted.
func (w *Workspace) CookiesPath() string { return filepath.Join(w.Dir, cookiesFile) }

// Settings are user preferences.
type Settings struct {
	TimeoutMs        int    `json:"timeoutMs"`
	FollowRedirects  bool   `json:"followRedirects"`
	MaxRedirects     int    `json:"maxRedirects"`
	SSLVerify        bool   `json:"sslVerify"`
	ProxyMode        string `json:"proxyMode"`
	ProxyURL         string `json:"proxyUrl"`
	MaxResponseMB    int    `json:"maxResponseMB"`
	Theme            string `json:"theme"`
	SaveCookies      bool   `json:"saveCookies"`
	HistoryLimit     int    `json:"historyLimit"`
	ScriptTimeoutSec int    `json:"scriptTimeoutSec"`
	FontSize         int    `json:"fontSize"`
	EditorTabSize    int    `json:"editorTabSize"`
	PersistVariables bool   `json:"persistVariables"`
}

// DefaultSettings returns defaults.
func DefaultSettings() Settings {
	return Settings{FollowRedirects: true, MaxRedirects: 10, SSLVerify: true, ProxyMode: "system", MaxResponseMB: 100, Theme: "system", SaveCookies: true, HistoryLimit: 200, ScriptTimeoutSec: 60, FontSize: 13, EditorTabSize: 2, PersistVariables: true}
}

// Settings loads settings merged over defaults.
func (w *Workspace) Settings() Settings {
	s := DefaultSettings()
	_ = w.loadJSON(settingsFile, &s)
	return s
}

// SaveSettings stores settings.
func (w *Workspace) SaveSettings(s Settings) error { return w.saveJSON(settingsFile, s) }

// HistoryEntry is a sent request.
type HistoryEntry struct {
	ID       string          `json:"id"`
	Time     time.Time       `json:"time"`
	Name     string          `json:"name,omitempty"`
	Method   string          `json:"method"`
	URL      string          `json:"url"`
	Code     int             `json:"code,omitempty"`
	Duration float64         `json:"duration,omitempty"`
	Request  json.RawMessage `json:"request"` // Postman request JSON (unresolved)
}

// History returns history entries, newest first.
func (w *Workspace) History() []HistoryEntry {
	var h []HistoryEntry
	_ = w.loadJSON(historyFile, &h)
	if h == nil {
		h = []HistoryEntry{}
	}
	return h
}

// AddHistory prepends an entry, keeping at most limit entries.
func (w *Workspace) AddHistory(e HistoryEntry, limit int) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	h := append([]HistoryEntry{e}, w.History()...)
	if limit > 0 && len(h) > limit {
		h = h[:limit]
	}
	return w.saveJSON(historyFile, h)
}

// DeleteHistory removes one entry, or all when id is empty.
func (w *Workspace) DeleteHistory(id string) error {
	if id == "" {
		return w.saveJSON(historyFile, []HistoryEntry{})
	}
	var out []HistoryEntry
	for _, e := range w.History() {
		if e.ID != id {
			out = append(out, e)
		}
	}
	if out == nil {
		out = []HistoryEntry{}
	}
	return w.saveJSON(historyFile, out)
}

// UIState is opaque frontend state (open tabs, selected environment, ...).
func (w *Workspace) UIState() json.RawMessage {
	var raw json.RawMessage
	_ = w.loadJSON(stateFile, &raw)
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	return raw
}

// SaveUIState stores frontend state.
func (w *Workspace) SaveUIState(raw json.RawMessage) error { return w.saveJSON(stateFile, raw) }
