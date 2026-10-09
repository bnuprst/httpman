package runner

import (
	"encoding/json"
	"strings"

	"github.com/bnuprst/httpman/internal/collection"
	"github.com/bnuprst/httpman/internal/httpclient"
	"github.com/bnuprst/httpman/internal/vars"
)

// EffectiveAuth returns the auth that applies to an item, following
// Postman's inheritance: request → nearest folder → collection.
func EffectiveAuth(c *collection.Collection, item *collection.Item, parents []*collection.Item) *collection.Auth {
	inherit := func(a *collection.Auth) bool { return a == nil || a.Type == "" || a.Type == "inherit" }
	if item != nil && item.Request != nil && !inherit(item.Request.Auth) {
		return item.Request.Auth
	}
	for i := len(parents) - 1; i >= 0; i-- {
		if !inherit(parents[i].Auth) {
			return parents[i].Auth
		}
	}
	if c != nil && !inherit(c.Auth) {
		return c.Auth
	}
	return nil
}

// ProtocolProfile merges protocolProfileBehavior from collection → folders → item.
func ProtocolProfile(c *collection.Collection, item *collection.Item, parents []*collection.Item) httpclient.RequestOverrides {
	var ov httpclient.RequestOverrides
	apply := func(raw json.RawMessage) {
		if len(raw) == 0 {
			return
		}
		var p struct {
			FollowRedirects *bool `json:"followRedirects"`
			MaxRedirects    *int  `json:"maxRedirects"`
			StrictSSL       *bool `json:"strictSSL"`
			FollowOriginal  *bool `json:"followOriginalHttpMethod"`
			RemoveReferer   *bool `json:"removeRefererHeaderOnRedirect"`
		}
		if json.Unmarshal(raw, &p) == nil {
			if p.FollowRedirects != nil {
				ov.FollowRedirects = p.FollowRedirects
			}
			if p.MaxRedirects != nil {
				ov.MaxRedirects = p.MaxRedirects
			}
			if p.StrictSSL != nil {
				ov.StrictSSL = p.StrictSSL
			}
			if p.FollowOriginal != nil {
				ov.FollowOriginalMethod = p.FollowOriginal
			}
			if p.RemoveReferer != nil {
				ov.RemoveRefererOnRedirect = p.RemoveReferer
			}
		}
	}
	if c != nil {
		apply(c.ProtocolProfileBehavior)
	}
	for _, p := range parents {
		apply(p.ProtocolProfileBehavior)
	}
	if item != nil {
		apply(item.ProtocolProfileBehavior)
	}
	return ov
}

// Resolve substitutes variables throughout a request, producing a request
// ready to send.
func Resolve(r *collection.Request, res vars.Resolver) *httpclient.Request {
	rep := res.Replace
	out := &httpclient.Request{Method: rep(r.Method)}

	// Path variables are resolved first so that values may contain {{vars}}.
	pathVars := map[string]string{}
	for _, v := range r.URL.Variable {
		if v.Disabled {
			continue
		}
		pathVars[v.Key] = rep(collection.ValueString(v.Value))
	}
	out.URL = substitutePathVars(rep(r.URL.String()), pathVars)

	for _, h := range r.Header {
		if h.Disabled {
			continue
		}
		out.Header = append(out.Header, collection.Header{Key: rep(h.Key), Value: rep(h.Value)})
	}
	if r.Body != nil {
		b := *r.Body
		b.Raw = rep(b.Raw)
		b.URLEncoded = resolveKVs(b.URLEncoded, rep)
		b.FormData = resolveKVs(b.FormData, rep)
		if b.GraphQL != nil {
			g := *b.GraphQL
			g.Query = rep(g.Query)
			g.Variables = rep(g.Variables)
			b.GraphQL = &g
		}
		if b.File != nil {
			f := *b.File
			f.Src = rep(f.Src)
			b.File = &f
		}
		out.Body = &b
	}
	if r.Auth != nil {
		a := &collection.Auth{Type: r.Auth.Type, Params: map[string][]collection.AuthParam{}}
		for k, list := range r.Auth.Params {
			if k != r.Auth.Type {
				continue
			}
			for _, p := range list {
				if s, ok := p.Value.(string); ok {
					p.Value = rep(s)
				}
				a.Params[k] = append(a.Params[k], p)
			}
		}
		out.Auth = a
	}
	return out
}

func resolveKVs(in []collection.KV, rep func(string) string) []collection.KV {
	if in == nil {
		return nil
	}
	out := make([]collection.KV, 0, len(in))
	for _, kv := range in {
		if kv.Disabled {
			continue
		}
		kv.Key = rep(kv.Key)
		kv.Value = rep(kv.Value)
		kv.ContentType = rep(kv.ContentType)
		switch s := kv.Src.(type) {
		case string:
			kv.Src = rep(s)
		case []any:
			list := make([]any, len(s))
			for i, v := range s {
				if str, ok := v.(string); ok {
					list[i] = rep(str)
				} else {
					list[i] = v
				}
			}
			kv.Src = list
		}
		out = append(out, kv)
	}
	return out
}

// substitutePathVars replaces :name path segments with their values.
func substitutePathVars(raw string, values map[string]string) string {
	if len(values) == 0 || !strings.Contains(raw, "/:") {
		return raw
	}
	start := 0
	if i := strings.Index(raw, "://"); i >= 0 {
		start = i + 3
	}
	slash := strings.IndexByte(raw[start:], '/')
	if slash < 0 {
		return raw
	}
	start += slash
	end := len(raw)
	if i := strings.IndexAny(raw[start:], "?#"); i >= 0 {
		end = start + i
	}
	segs := strings.Split(raw[start:end], "/")
	for i, seg := range segs {
		if strings.HasPrefix(seg, ":") {
			if v, ok := values[seg[1:]]; ok {
				segs[i] = v
			}
		}
	}
	return raw[:start] + strings.Join(segs, "/") + raw[end:]
}
