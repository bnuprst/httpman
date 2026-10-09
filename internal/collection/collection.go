// Package collection implements the Postman Collection format (v2.0 and v2.1)
// with lenient parsing, plus conversion of legacy v1 collections.
//
// The canonical in-memory and on-disk representation is v2.1. Parsing accepts
// the many shapes that real-world exports contain (url as string or object,
// auth params as map or key/value list, script exec as string or lines, ...).
package collection

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	SchemaV21 = "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"
	SchemaV20 = "https://schema.getpostman.com/json/collection/v2.0.0/collection.json"
)

// Collection is a Postman collection.
type Collection struct {
	Info                    Info            `json:"info"`
	Item                    []*Item         `json:"item"`
	Event                   []Event         `json:"event,omitempty"`
	Variable                []Variable      `json:"variable,omitempty"`
	Auth                    *Auth           `json:"auth,omitempty"`
	ProtocolProfileBehavior json.RawMessage `json:"protocolProfileBehavior,omitempty"`
}

// Info holds collection metadata.
type Info struct {
	PostmanID   string      `json:"_postman_id,omitempty"`
	Name        string      `json:"name"`
	Description Description `json:"description,omitempty"`
	Schema      string      `json:"schema"`
	ExporterID  string      `json:"_exporter_id,omitempty"`
}

// Item is either a request item or a folder (when Item is non-nil).
type Item struct {
	ID                      string          `json:"id,omitempty"`
	Name                    string          `json:"name"`
	Description             Description     `json:"description,omitempty"`
	Item                    []*Item         `json:"item,omitempty"`
	Request                 *Request        `json:"request,omitempty"`
	Response                json.RawMessage `json:"response,omitempty"`
	Event                   []Event         `json:"event,omitempty"`
	Variable                []Variable      `json:"variable,omitempty"`
	Auth                    *Auth           `json:"auth,omitempty"`
	ProtocolProfileBehavior json.RawMessage `json:"protocolProfileBehavior,omitempty"`
}

// IsFolder reports whether the item is a folder (item group).
func (it *Item) IsFolder() bool { return it.Request == nil && it.Item != nil }

func (it *Item) UnmarshalJSON(b []byte) error {
	type alias Item
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*it = Item(a)
	// A folder with zero children still has "item": [] — keep it a folder.
	if it.Request == nil && it.Item == nil {
		var probe map[string]json.RawMessage
		_ = json.Unmarshal(b, &probe)
		if _, ok := probe["item"]; ok {
			it.Item = []*Item{}
		}
	}
	return nil
}

// Description can be a plain string or {content, type, version}.
type Description string

func (d *Description) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*d = ""
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*d = Description(s)
		return nil
	}
	var obj struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return nil // ignore unparseable descriptions
	}
	*d = Description(obj.Content)
	return nil
}

// Event is a script attached to a lifecycle event ("prerequest" or "test").
type Event struct {
	ID       string `json:"id,omitempty"`
	Listen   string `json:"listen"`
	Script   Script `json:"script"`
	Disabled bool   `json:"disabled,omitempty"`
}

// Script holds JavaScript source lines.
type Script struct {
	ID   string          `json:"id,omitempty"`
	Type string          `json:"type,omitempty"`
	Exec ExecLines       `json:"exec"`
	Src  json.RawMessage `json:"src,omitempty"`
	Name string          `json:"name,omitempty"`
}

// ExecLines accepts either a string or an array of strings.
type ExecLines []string

func (e *ExecLines) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*e = nil
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*e = strings.Split(s, "\n")
		return nil
	}
	var lines []string
	if err := json.Unmarshal(b, &lines); err != nil {
		return err
	}
	*e = lines
	return nil
}

// Source returns the script as a single string.
func (s Script) Source() string { return strings.Join(s.Exec, "\n") }

// ScriptFor returns the joined source of all enabled scripts for an event.
func ScriptFor(events []Event, listen string) string {
	var parts []string
	for _, ev := range events {
		if ev.Listen == listen && !ev.Disabled {
			if src := ev.Script.Source(); strings.TrimSpace(src) != "" {
				parts = append(parts, src)
			}
		}
	}
	return strings.Join(parts, "\n;\n")
}

// Variable is a collection/folder variable or a url path variable.
type Variable struct {
	ID          string      `json:"id,omitempty"`
	Key         string      `json:"key"`
	Value       any         `json:"value"`
	Type        string      `json:"type,omitempty"`
	Name        string      `json:"name,omitempty"`
	Description Description `json:"description,omitempty"`
	Disabled    bool        `json:"disabled,omitempty"`
	System      bool        `json:"system,omitempty"`
}

// Request describes an HTTP request.
type Request struct {
	URL         URL             `json:"url"`
	Auth        *Auth           `json:"auth,omitempty"`
	Method      string          `json:"method"`
	Description Description     `json:"description,omitempty"`
	Header      Headers         `json:"header"`
	Body        *Body           `json:"body,omitempty"`
	Proxy       json.RawMessage `json:"proxy,omitempty"`
	Certificate json.RawMessage `json:"certificate,omitempty"`
}

func (r *Request) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	// A request may be just a URL string.
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*r = Request{Method: "GET", URL: URL{Raw: s}}
		return nil
	}
	type alias Request
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*r = Request(a)
	if r.Method == "" {
		r.Method = "GET"
	}
	r.Method = strings.ToUpper(r.Method)
	return nil
}

// Header is an HTTP header entry.
type Header struct {
	Key         string      `json:"key"`
	Value       string      `json:"value"`
	Disabled    bool        `json:"disabled,omitempty"`
	Type        string      `json:"type,omitempty"`
	Description Description `json:"description,omitempty"`
}

// Headers accepts an array of header objects or a raw "K: V\n" string.
type Headers []Header

func (h *Headers) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*h = nil
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*h = ParseHeaderString(s)
		return nil
	}
	var list []Header
	if err := json.Unmarshal(b, &list); err != nil {
		return err
	}
	*h = list
	return nil
}

// ParseHeaderString parses "Key: Value" lines.
func ParseHeaderString(s string) Headers {
	var out Headers
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		disabled := false
		if strings.HasPrefix(line, "//") {
			disabled = true
			line = strings.TrimSpace(line[2:])
		}
		k, v, _ := strings.Cut(line, ":")
		out = append(out, Header{Key: strings.TrimSpace(k), Value: strings.TrimSpace(v), Disabled: disabled})
	}
	return out
}

// QueryParam is a URL query parameter.
type QueryParam struct {
	Key         *string     `json:"key"`
	Value       *string     `json:"value"`
	Disabled    bool        `json:"disabled,omitempty"`
	Description Description `json:"description,omitempty"`
}

// URL is a Postman URL. It can be a string or an object in JSON.
type URL struct {
	Raw      string       `json:"raw"`
	Protocol string       `json:"protocol,omitempty"`
	Host     []string     `json:"host,omitempty"`
	Path     []string     `json:"path,omitempty"`
	Port     string       `json:"port,omitempty"`
	Query    []QueryParam `json:"query,omitempty"`
	Hash     string       `json:"hash,omitempty"`
	Variable []Variable   `json:"variable,omitempty"`
}

func (u *URL) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*u = URL{}
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*u = URL{Raw: s}
		return nil
	}
	var raw struct {
		Raw      string          `json:"raw"`
		Protocol string          `json:"protocol"`
		Host     json.RawMessage `json:"host"`
		Path     json.RawMessage `json:"path"`
		Port     json.RawMessage `json:"port"`
		Query    []QueryParam    `json:"query"`
		Hash     string          `json:"hash"`
		Variable []Variable      `json:"variable"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*u = URL{Raw: raw.Raw, Protocol: raw.Protocol, Query: raw.Query, Hash: raw.Hash, Variable: raw.Variable}
	u.Host = stringOrList(raw.Host, ".")
	u.Path = stringOrList(raw.Path, "/")
	if len(raw.Port) > 0 {
		var s string
		if json.Unmarshal(raw.Port, &s) == nil {
			u.Port = s
		} else {
			var n json.Number
			if json.Unmarshal(raw.Port, &n) == nil {
				u.Port = n.String()
			}
		}
	}
	return nil
}

func stringOrList(b json.RawMessage, sep string) []string {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '"' {
		var s string
		_ = json.Unmarshal(b, &s)
		s = strings.Trim(s, sep)
		if s == "" {
			return nil
		}
		return strings.Split(s, sep)
	}
	var items []json.RawMessage
	if json.Unmarshal(b, &items) != nil {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		var s string
		if json.Unmarshal(it, &s) == nil {
			out = append(out, s)
			continue
		}
		// Path segments can be {type, value} objects.
		var obj struct {
			Value string `json:"value"`
		}
		if json.Unmarshal(it, &obj) == nil {
			out = append(out, obj.Value)
		}
	}
	return out
}

// String renders the URL. Raw is authoritative when present because it is
// what the user edits; structured parts are used as a fallback.
func (u URL) String() string {
	if u.Raw != "" {
		return u.Raw
	}
	var sb strings.Builder
	if u.Protocol != "" {
		sb.WriteString(u.Protocol)
		sb.WriteString("://")
	}
	sb.WriteString(strings.Join(u.Host, "."))
	if u.Port != "" {
		sb.WriteString(":" + u.Port)
	}
	if len(u.Path) > 0 {
		sb.WriteString("/" + strings.Join(u.Path, "/"))
	}
	var qs []string
	for _, q := range u.Query {
		if q.Disabled || q.Key == nil {
			continue
		}
		s := *q.Key
		if q.Value != nil {
			s += "=" + *q.Value
		}
		qs = append(qs, s)
	}
	if len(qs) > 0 {
		sb.WriteString("?" + strings.Join(qs, "&"))
	}
	if u.Hash != "" {
		sb.WriteString("#" + u.Hash)
	}
	return sb.String()
}

// KV is a generic key/value parameter (used by bodies).
type KV struct {
	Key         string      `json:"key"`
	Value       string      `json:"value,omitempty"`
	Disabled    bool        `json:"disabled,omitempty"`
	Type        string      `json:"type,omitempty"` // formdata: "text" | "file"
	Src         any         `json:"src,omitempty"`  // formdata file: string or []string
	ContentType string      `json:"contentType,omitempty"`
	Description Description `json:"description,omitempty"`
}

// Files returns the file paths referenced by a formdata file entry.
func (kv KV) Files() []string {
	switch v := kv.Src.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case []any:
		var out []string
		for _, s := range v {
			if str, ok := s.(string); ok && str != "" {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}

// Body is a request body.
type Body struct {
	Mode       string          `json:"mode"`
	Raw        string          `json:"raw,omitempty"`
	URLEncoded []KV            `json:"urlencoded,omitempty"`
	FormData   []KV            `json:"formdata,omitempty"`
	File       *BodyFile       `json:"file,omitempty"`
	GraphQL    *GraphQL        `json:"graphql,omitempty"`
	Options    json.RawMessage `json:"options,omitempty"`
	Disabled   bool            `json:"disabled,omitempty"`
}

// BodyFile references a file used as the binary body.
type BodyFile struct {
	Src     string `json:"src,omitempty"`
	Content string `json:"content,omitempty"`
}

// GraphQL body.
type GraphQL struct {
	Query     string `json:"query"`
	Variables string `json:"variables,omitempty"`
}

func (g *GraphQL) UnmarshalJSON(b []byte) error {
	var raw struct {
		Query     string          `json:"query"`
		Variables json.RawMessage `json:"variables"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	g.Query = raw.Query
	v := bytes.TrimSpace(raw.Variables)
	if len(v) > 0 && v[0] == '"' {
		_ = json.Unmarshal(v, &g.Variables)
	} else if len(v) > 0 && string(v) != "null" {
		g.Variables = string(v)
	}
	return nil
}

// RawLanguage returns options.raw.language ("json", "xml", "text", ...).
func (b *Body) RawLanguage() string {
	if b == nil || len(b.Options) == 0 {
		return ""
	}
	var o struct {
		Raw struct {
			Language string `json:"language"`
		} `json:"raw"`
	}
	_ = json.Unmarshal(b.Options, &o)
	return o.Raw.Language
}

// AuthParam is a single auth attribute (v2.1 style).
type AuthParam struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
	Type  string `json:"type,omitempty"`
}

// Auth describes request authentication. Params are normalized from both
// v2.0 ({"basic": {"username": ...}}) and v2.1 ({"basic": [{key, value}]}).
type Auth struct {
	Type   string
	Params map[string][]AuthParam
}

func (a *Auth) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	a.Params = map[string][]AuthParam{}
	if t, ok := raw["type"]; ok {
		_ = json.Unmarshal(t, &a.Type)
	}
	for k, v := range raw {
		if k == "type" {
			continue
		}
		v = bytes.TrimSpace(v)
		if len(v) == 0 {
			continue
		}
		switch v[0] {
		case '[':
			var list []AuthParam
			if json.Unmarshal(v, &list) == nil {
				a.Params[k] = list
			}
		case '{':
			var m map[string]any
			if json.Unmarshal(v, &m) == nil {
				list := make([]AuthParam, 0, len(m))
				for mk, mv := range m {
					list = append(list, AuthParam{Key: mk, Value: mv, Type: "string"})
				}
				a.Params[k] = list
			}
		}
	}
	return nil
}

func (a Auth) MarshalJSON() ([]byte, error) {
	m := map[string]any{"type": a.Type}
	for k, v := range a.Params {
		m[k] = v
	}
	return json.Marshal(m)
}

// Get returns an auth parameter of the active type as a string.
func (a *Auth) Get(key string) string {
	if a == nil {
		return ""
	}
	for _, p := range a.Params[a.Type] {
		if p.Key == key {
			return ValueString(p.Value)
		}
	}
	return ""
}

// ValueString converts a JSON value into the string Postman would use.
func ValueString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		return formatNumber(t)
	case json.Number:
		return t.String()
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprint(t)
		}
		return string(b)
	}
}

func formatNumber(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

// Walk visits every item in depth-first order with its ancestor chain.
func (c *Collection) Walk(fn func(it *Item, parents []*Item) bool) {
	var walk func(items []*Item, parents []*Item) bool
	walk = func(items []*Item, parents []*Item) bool {
		for _, it := range items {
			if !fn(it, parents) {
				return false
			}
			if it.IsFolder() {
				p := append(append([]*Item{}, parents...), it)
				if !walk(it.Item, p) {
					return false
				}
			}
		}
		return true
	}
	walk(c.Item, nil)
}

// Find returns the item with the given id (or name as fallback) and its parents.
func (c *Collection) Find(idOrName string) (*Item, []*Item) {
	var found *Item
	var chain []*Item
	c.Walk(func(it *Item, parents []*Item) bool {
		if it.ID != "" && it.ID == idOrName {
			found, chain = it, parents
			return false
		}
		return true
	})
	if found == nil {
		c.Walk(func(it *Item, parents []*Item) bool {
			if it.Name == idOrName {
				found, chain = it, parents
				return false
			}
			return true
		})
	}
	return found, chain
}

// Requests returns all request items (not folders) in execution order.
func Requests(items []*Item) []*Item {
	var out []*Item
	for _, it := range items {
		if it.IsFolder() {
			out = append(out, Requests(it.Item)...)
		} else if it.Request != nil {
			out = append(out, it)
		}
	}
	return out
}
