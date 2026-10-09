package collection

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// Parse reads a collection in v1, v2.0 or v2.1 format and returns it
// normalized to v2.1, with ids assigned to every item.
func Parse(data []byte) (*Collection, error) {
	data = bytes.TrimPrefix(bytes.TrimSpace(data), []byte("\xef\xbb\xbf"))
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	// Some exports wrap the collection: {"collection": {...}} (Postman API format).
	if inner, ok := probe["collection"]; ok && probe["info"] == nil {
		return Parse(inner)
	}
	var c *Collection
	switch {
	case probe["info"] != nil:
		c = &Collection{}
		if err := json.Unmarshal(data, c); err != nil {
			return nil, fmt.Errorf("invalid collection: %w", err)
		}
	case probe["requests"] != nil || probe["order"] != nil:
		var err error
		c, err = convertV1(data)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("not a Postman collection (missing \"info\" or \"requests\")")
	}
	Normalize(c)
	return c, nil
}

// Normalize fills defaults: schema, ids, non-nil item lists.
func Normalize(c *Collection) {
	c.Info.Schema = SchemaV21
	if c.Info.PostmanID == "" {
		c.Info.PostmanID = uuid.NewString()
	}
	if c.Item == nil {
		c.Item = []*Item{}
	}
	seen := map[string]bool{}
	c.Walk(func(it *Item, _ []*Item) bool {
		if it.ID == "" || seen[it.ID] {
			it.ID = uuid.NewString()
		}
		seen[it.ID] = true
		if it.Request != nil && it.Request.Header == nil {
			it.Request.Header = Headers{}
		}
		return true
	})
}

// Marshal serializes a collection as indented v2.1 JSON.
func Marshal(c *Collection) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "\t")
	if err := enc.Encode(c); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ---- v1 conversion ----

type v1Collection struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	Description  string      `json:"description"`
	Order        []string    `json:"order"`
	FoldersOrder []string    `json:"folders_order"`
	Folders      []v1Folder  `json:"folders"`
	Requests     []v1Request `json:"requests"`
	Events       []Event     `json:"events"`
	Variables    []Variable  `json:"variables"`
	Auth         *Auth       `json:"auth"`
}

type v1Folder struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Order        []string `json:"order"`
	FoldersOrder []string `json:"folders_order"`
	Folder       string   `json:"folder"`
	Events       []Event  `json:"events"`
	Auth         *Auth    `json:"auth"`
}

type v1KV struct {
	Key         string `json:"key"`
	Value       any    `json:"value"`
	Type        string `json:"type"`
	Enabled     *bool  `json:"enabled"`
	Description string `json:"description"`
}

func (kv v1KV) disabled() bool { return kv.Enabled != nil && !*kv.Enabled }

type v1Request struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	URL              string          `json:"url"`
	Method           string          `json:"method"`
	Headers          string          `json:"headers"`
	HeaderData       []v1KV          `json:"headerData"`
	QueryParams      []v1KV          `json:"queryParams"`
	PathVariableData []v1KV          `json:"pathVariableData"`
	PathVariables    map[string]any  `json:"pathVariables"`
	DataMode         string          `json:"dataMode"`
	Data             json.RawMessage `json:"data"`
	RawModeData      string          `json:"rawModeData"`
	GraphQLModeData  *GraphQL        `json:"graphqlModeData"`
	PreRequestScript string          `json:"preRequestScript"`
	Tests            string          `json:"tests"`
	Events           []Event         `json:"events"`
	Auth             *Auth           `json:"auth"`
	CurrentHelper    string          `json:"currentHelper"`
	HelperAttributes map[string]any  `json:"helperAttributes"`
	Folder           string          `json:"folder"`
	Responses        json.RawMessage `json:"responses"`
}

func convertV1(data []byte) (*Collection, error) {
	var v1 v1Collection
	if err := json.Unmarshal(data, &v1); err != nil {
		return nil, fmt.Errorf("invalid v1 collection: %w", err)
	}
	c := &Collection{
		Info:     Info{PostmanID: v1.ID, Name: v1.Name, Description: Description(v1.Description)},
		Event:    v1.Events,
		Variable: v1.Variables,
		Auth:     v1.Auth,
	}
	reqs := map[string]*v1Request{}
	for i := range v1.Requests {
		reqs[v1.Requests[i].ID] = &v1.Requests[i]
	}
	folders := map[string]*v1Folder{}
	for i := range v1.Folders {
		folders[v1.Folders[i].ID] = &v1.Folders[i]
	}
	used := map[string]bool{}
	usedFolders := map[string]bool{}

	var buildFolder func(f *v1Folder) *Item
	buildFolder = func(f *v1Folder) *Item {
		usedFolders[f.ID] = true
		it := &Item{ID: f.ID, Name: f.Name, Description: Description(f.Description), Event: f.Events, Auth: f.Auth, Item: []*Item{}}
		for _, fid := range f.FoldersOrder {
			if sub, ok := folders[fid]; ok && !usedFolders[fid] {
				it.Item = append(it.Item, buildFolder(sub))
			}
		}
		for _, rid := range f.Order {
			if r, ok := reqs[rid]; ok && !used[rid] {
				used[rid] = true
				it.Item = append(it.Item, convertV1Request(r))
			}
		}
		return it
	}

	rootFolders := v1.FoldersOrder
	if len(rootFolders) == 0 {
		// Old exports lack folders_order: take every folder not nested elsewhere.
		nested := map[string]bool{}
		for _, f := range v1.Folders {
			for _, id := range f.FoldersOrder {
				nested[id] = true
			}
		}
		for _, f := range v1.Folders {
			if !nested[f.ID] {
				rootFolders = append(rootFolders, f.ID)
			}
		}
	}
	for _, fid := range rootFolders {
		if f, ok := folders[fid]; ok && !usedFolders[fid] {
			c.Item = append(c.Item, buildFolder(f))
		}
	}
	for _, rid := range v1.Order {
		if r, ok := reqs[rid]; ok && !used[rid] {
			used[rid] = true
			c.Item = append(c.Item, convertV1Request(r))
		}
	}
	// Requests not referenced by any order list are appended at the root.
	for i := range v1.Requests {
		r := &v1.Requests[i]
		if !used[r.ID] {
			used[r.ID] = true
			c.Item = append(c.Item, convertV1Request(r))
		}
	}
	return c, nil
}

func convertV1Request(r *v1Request) *Item {
	it := &Item{ID: r.ID, Name: r.Name, Description: Description(r.Description)}
	if it.Name == "" {
		it.Name = r.URL
	}
	req := &Request{Method: strings.ToUpper(r.Method), URL: URL{Raw: r.URL}, Auth: r.Auth}
	if req.Method == "" {
		req.Method = "GET"
	}
	if len(r.HeaderData) > 0 {
		for _, h := range r.HeaderData {
			req.Header = append(req.Header, Header{Key: h.Key, Value: ValueString(h.Value), Disabled: h.disabled(), Description: Description(h.Description)})
		}
	} else {
		req.Header = ParseHeaderString(r.Headers)
	}
	if len(r.PathVariableData) > 0 {
		for _, v := range r.PathVariableData {
			req.URL.Variable = append(req.URL.Variable, Variable{Key: v.Key, Value: ValueString(v.Value)})
		}
	} else {
		for k, v := range r.PathVariables {
			req.URL.Variable = append(req.URL.Variable, Variable{Key: k, Value: ValueString(v)})
		}
	}

	var kvs []v1KV
	if len(r.Data) > 0 && r.Data[0] == '[' {
		_ = json.Unmarshal(r.Data, &kvs)
	}
	switch r.DataMode {
	case "raw":
		raw := r.RawModeData
		if raw == "" && len(r.Data) > 0 && r.Data[0] == '"' {
			_ = json.Unmarshal(r.Data, &raw)
		}
		req.Body = &Body{Mode: "raw", Raw: raw}
	case "urlencoded":
		b := &Body{Mode: "urlencoded"}
		for _, kv := range kvs {
			b.URLEncoded = append(b.URLEncoded, KV{Key: kv.Key, Value: ValueString(kv.Value), Disabled: kv.disabled()})
		}
		req.Body = b
	case "params":
		b := &Body{Mode: "formdata"}
		for _, kv := range kvs {
			e := KV{Key: kv.Key, Type: "text", Disabled: kv.disabled()}
			if kv.Type == "file" {
				e.Type = "file"
				e.Src = ValueString(kv.Value)
			} else {
				e.Value = ValueString(kv.Value)
			}
			b.FormData = append(b.FormData, e)
		}
		req.Body = b
	case "binary":
		req.Body = &Body{Mode: "file", File: &BodyFile{}}
	case "graphql":
		req.Body = &Body{Mode: "graphql", GraphQL: r.GraphQLModeData}
	}

	if req.Auth == nil && r.CurrentHelper != "" && r.CurrentHelper != "normal" {
		req.Auth = convertV1Helper(r.CurrentHelper, r.HelperAttributes)
	}

	it.Request = req
	it.Event = r.Events
	if len(it.Event) == 0 {
		if strings.TrimSpace(r.PreRequestScript) != "" {
			it.Event = append(it.Event, Event{Listen: "prerequest", Script: Script{Type: "text/javascript", Exec: strings.Split(r.PreRequestScript, "\n")}})
		}
		if strings.TrimSpace(r.Tests) != "" {
			it.Event = append(it.Event, Event{Listen: "test", Script: Script{Type: "text/javascript", Exec: strings.Split(r.Tests, "\n")}})
		}
	}
	return it
}

func convertV1Helper(helper string, attrs map[string]any) *Auth {
	t := map[string]string{
		"basicAuth":  "basic",
		"digestAuth": "digest",
		"bearerAuth": "bearer",
		"oAuth1":     "oauth1",
		"oAuth2":     "oauth2",
		"hawkAuth":   "hawk",
		"awsSigV4":   "awsv4",
		"ntlmAuth":   "ntlm",
	}[helper]
	if t == "" {
		return nil
	}
	var params []AuthParam
	for k, v := range attrs {
		if k == "id" || k == "time" || k == "saveToRequest" {
			continue
		}
		params = append(params, AuthParam{Key: k, Value: v, Type: "string"})
	}
	return &Auth{Type: t, Params: map[string][]AuthParam{t: params}}
}
