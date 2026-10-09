package collection

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"
)

// NormalizeRaw prepares collection JSON for storage while preserving every
// field it does not understand. v2.0/v2.1 input is normalized in place (ids,
// schema, v2.1 auth layout); v1 input is converted through the typed model.
// It returns the normalized JSON and the parsed collection.
func NormalizeRaw(data []byte) ([]byte, *Collection, error) {
	c, err := Parse(data)
	if err != nil {
		return nil, nil, err
	}
	data = DecodeText(data)
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var root map[string]any
	if err := dec.Decode(&root); err != nil {
		return nil, nil, err
	}
	if inner, ok := root["collection"].(map[string]any); ok && root["info"] == nil {
		root = inner
	}
	info, ok := root["info"].(map[string]any)
	if !ok && root["item"] != nil {
		info = map[string]any{"name": c.Info.Name, "_postman_id": c.Info.PostmanID}
		delete(root, "name")
		delete(root, "id")
		root["info"] = info
		ok = true
	}
	if !ok {
		// v1: no extra fields worth preserving; use the converted model.
		out, err := Marshal(c)
		return out, c, err
	}
	info["schema"] = SchemaV21
	if id, _ := info["_postman_id"].(string); id == "" {
		info["_postman_id"] = c.Info.PostmanID
	}
	normalizeAuth(root)
	normalizeLists(root)
	if _, ok := root["item"].([]any); !ok {
		root["item"] = []any{}
	}
	seen := map[string]bool{}
	var walk func(items []any)
	walk = func(items []any) {
		for _, raw := range items {
			it, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			id, _ := it["id"].(string)
			if id == "" || seen[id] {
				id = uuid.NewString()
				it["id"] = id
			}
			seen[id] = true
			normalizeAuth(it)
			normalizeLists(it)
			switch req := it["request"].(type) {
			case string:
				it["request"] = map[string]any{"method": "GET", "url": map[string]any{"raw": req}, "header": []any{}}
			case map[string]any:
				normalizeAuth(req)
				if u, ok := req["url"].(string); ok {
					req["url"] = map[string]any{"raw": u}
				}
				if _, ok := req["header"].([]any); !ok {
					if s, ok := req["header"].(string); ok {
						var hs []any
						for _, h := range ParseHeaderString(s) {
							hs = append(hs, map[string]any{"key": h.Key, "value": h.Value, "disabled": h.Disabled})
						}
						req["header"] = hs
					} else {
						req["header"] = []any{}
					}
				}
			}
			if children, ok := it["item"].([]any); ok {
				walk(children)
			}
		}
	}
	walk(root["item"].([]any))

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "\t")
	if err := enc.Encode(root); err != nil {
		return nil, nil, fmt.Errorf("encoding collection: %w", err)
	}
	// Re-parse so the returned model matches the stored ids.
	c, err = Parse(buf.Bytes())
	return buf.Bytes(), c, err
}

// normalizeAuth converts v2.0 auth params ({"basic": {"username": ..}}) to
// the v2.1 list form ({"basic": [{"key": "username", ...}]}).
func normalizeAuth(obj map[string]any) {
	auth, ok := obj["auth"].(map[string]any)
	if !ok {
		return
	}
	for k, v := range auth {
		if k == "type" {
			continue
		}
		if m, ok := v.(map[string]any); ok {
			list := make([]any, 0, len(m))
			for pk, pv := range m {
				list = append(list, map[string]any{"key": pk, "value": pv, "type": "string"})
			}
			auth[k] = list
		}
	}
}

// normalizeLists turns single-object "event" and map-style "variable" values
// into the list forms of the v2.1 schema.
func normalizeLists(obj map[string]any) {
	if ev, ok := obj["event"].(map[string]any); ok {
		obj["event"] = []any{ev}
	}
	if vars, ok := obj["variable"].(map[string]any); ok {
		keys := make([]string, 0, len(vars))
		for k := range vars {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		list := make([]any, 0, len(keys))
		for _, k := range keys {
			list = append(list, map[string]any{"key": k, "value": vars[k]})
		}
		obj["variable"] = list
	}
}
