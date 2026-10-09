// Package vars implements Postman variable scopes and {{variable}} substitution.
package vars

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/bnuprst/httpman/internal/collection"
)

// Var is a variable in an environment, globals or any other scope.
type Var struct {
	Key     string `json:"key"`
	Value   any    `json:"value"`
	Type    string `json:"type,omitempty"`
	Enabled bool   `json:"enabled"`
}

func (v *Var) UnmarshalJSON(b []byte) error {
	var raw struct {
		Key      string `json:"key"`
		Value    any    `json:"value"`
		Type     string `json:"type"`
		Enabled  *bool  `json:"enabled"`
		Disabled *bool  `json:"disabled"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*v = Var{Key: raw.Key, Value: raw.Value, Type: raw.Type, Enabled: true}
	if raw.Enabled != nil {
		v.Enabled = *raw.Enabled
	}
	if raw.Disabled != nil && *raw.Disabled {
		v.Enabled = false
	}
	return nil
}

// Scope is an ordered list of variables.
type Scope struct {
	Name string `json:"name"`
	Vars []Var  `json:"values"`
}

// NewScope creates a scope.
func NewScope(name string, vars []Var) *Scope {
	if vars == nil {
		vars = []Var{}
	}
	return &Scope{Name: name, Vars: vars}
}

// FromCollectionVars converts collection/folder variables into a scope.
func FromCollectionVars(name string, cv []collection.Variable) *Scope {
	s := NewScope(name, nil)
	for _, v := range cv {
		s.Vars = append(s.Vars, Var{Key: v.Key, Value: v.Value, Type: v.Type, Enabled: !v.Disabled})
	}
	return s
}

// FromMap builds a scope from a map (iteration data).
func FromMap(name string, m map[string]any, order []string) *Scope {
	s := NewScope(name, nil)
	if order == nil {
		for k := range m {
			order = append(order, k)
		}
	}
	for _, k := range order {
		s.Vars = append(s.Vars, Var{Key: k, Value: m[k], Enabled: true})
	}
	return s
}

// Get returns an enabled variable's value.
func (s *Scope) Get(key string) (any, bool) {
	if s == nil {
		return nil, false
	}
	for i := len(s.Vars) - 1; i >= 0; i-- {
		if s.Vars[i].Key == key && s.Vars[i].Enabled {
			return s.Vars[i].Value, true
		}
	}
	return nil, false
}

// Set adds or updates a variable.
func (s *Scope) Set(key string, value any) {
	for i := range s.Vars {
		if s.Vars[i].Key == key {
			s.Vars[i].Value = value
			s.Vars[i].Enabled = true
			return
		}
	}
	s.Vars = append(s.Vars, Var{Key: key, Value: value, Type: "default", Enabled: true})
}

// Unset removes a variable.
func (s *Scope) Unset(key string) {
	out := s.Vars[:0]
	for _, v := range s.Vars {
		if v.Key != key {
			out = append(out, v)
		}
	}
	s.Vars = out
}

// Clone returns a deep-enough copy (values are treated as immutable).
func (s *Scope) Clone() *Scope {
	if s == nil {
		return nil
	}
	return &Scope{Name: s.Name, Vars: append([]Var{}, s.Vars...)}
}

// Resolver looks up variables across scopes. Scopes are ordered from the
// lowest priority (globals) to the highest (local).
type Resolver struct {
	Scopes []*Scope
}

// Lookup finds a variable value as a string.
func (r Resolver) Lookup(key string) (string, bool) {
	for i := len(r.Scopes) - 1; i >= 0; i-- {
		if v, ok := r.Scopes[i].Get(key); ok {
			return collection.ValueString(v), true
		}
	}
	if strings.HasPrefix(key, "$") {
		if v, ok := Dynamic(key); ok {
			return v, true
		}
	}
	return "", false
}

var varPattern = regexp.MustCompile(`\{\{([^{}]+)\}\}`)

// Replace substitutes {{name}} occurrences. Unknown variables are left as is.
// Variables may reference other variables, up to a fixed depth.
func (r Resolver) Replace(s string) string {
	if !strings.Contains(s, "{{") {
		return s
	}
	for depth := 0; depth < 10; depth++ {
		changed := false
		s = varPattern.ReplaceAllStringFunc(s, func(m string) string {
			name := strings.TrimSpace(m[2 : len(m)-2])
			if v, ok := r.Lookup(name); ok {
				changed = true
				return v
			}
			return m
		})
		if !changed || !strings.Contains(s, "{{") {
			break
		}
	}
	return s
}

// ---- Environment files ----

// Environment is a Postman environment (or globals) file.
type Environment struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Values        []Var  `json:"values"`
	Scope         string `json:"_postman_variable_scope,omitempty"`
	ExportedAt    string `json:"_postman_exported_at,omitempty"`
	ExportedUsing string `json:"_postman_exported_using,omitempty"`
}

// ParseEnvironment reads a Postman environment or globals export.
func ParseEnvironment(data []byte) (*Environment, error) {
	data = bytes.TrimPrefix(bytes.TrimSpace(data), []byte("\xef\xbb\xbf"))
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if inner, ok := probe["environment"]; ok && probe["values"] == nil {
		return ParseEnvironment(inner)
	}
	if probe["values"] == nil {
		return nil, fmt.Errorf("not a Postman environment (missing \"values\")")
	}
	var env Environment
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	if env.Values == nil {
		env.Values = []Var{}
	}
	return &env, nil
}

// Scope converts the environment into a variable scope (sharing nothing).
func (e *Environment) ToScope(name string) *Scope {
	if e == nil {
		return NewScope(name, nil)
	}
	return NewScope(name, append([]Var{}, e.Values...))
}
