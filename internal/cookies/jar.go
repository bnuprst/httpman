// Package cookies provides a cookie jar that can be listed, edited and saved,
// unlike net/http/cookiejar.
package cookies

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"
)

// Cookie is a stored cookie.
type Cookie struct {
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Domain   string    `json:"domain"`
	Path     string    `json:"path"`
	Expires  time.Time `json:"expires,omitzero"`
	Secure   bool      `json:"secure,omitempty"`
	HttpOnly bool      `json:"httpOnly,omitempty"`
	HostOnly bool      `json:"hostOnly,omitempty"`
	SameSite string    `json:"sameSite,omitempty"`
}

func (c *Cookie) expired(now time.Time) bool {
	return !c.Expires.IsZero() && !c.Expires.After(now)
}

func (c *Cookie) key() string { return c.Domain + ";" + c.Path + ";" + c.Name }

// Jar is a thread-safe RFC 6265-ish cookie jar.
type Jar struct {
	mu       sync.Mutex
	cookies  map[string]*Cookie
	OnChange func()
}

// New creates an empty jar.
func New() *Jar { return &Jar{cookies: map[string]*Cookie{}} }

func canonicalHost(u *url.URL) string {
	h := strings.ToLower(u.Hostname())
	return strings.TrimSuffix(h, ".")
}

func defaultPath(u *url.URL) string {
	p := u.EscapedPath()
	if p == "" || p[0] != '/' {
		return "/"
	}
	i := strings.LastIndex(p, "/")
	if i == 0 {
		return "/"
	}
	return p[:i]
}

func domainMatch(host, domain string) bool {
	if host == domain {
		return true
	}
	return strings.HasSuffix(host, "."+domain) && net.ParseIP(host) == nil
}

func pathMatch(reqPath, cookiePath string) bool {
	if reqPath == "" {
		reqPath = "/"
	}
	if reqPath == cookiePath {
		return true
	}
	if strings.HasPrefix(reqPath, cookiePath) {
		return strings.HasSuffix(cookiePath, "/") || reqPath[len(cookiePath)] == '/'
	}
	return false
}

// SetCookies implements http.CookieJar.
func (j *Jar) SetCookies(u *url.URL, cs []*http.Cookie) {
	host := canonicalHost(u)
	now := time.Now()
	j.mu.Lock()
	changed := false
	for _, hc := range cs {
		c := &Cookie{Name: hc.Name, Value: hc.Value, Path: hc.Path, Secure: hc.Secure, HttpOnly: hc.HttpOnly}
		switch hc.SameSite {
		case http.SameSiteLaxMode:
			c.SameSite = "Lax"
		case http.SameSiteStrictMode:
			c.SameSite = "Strict"
		case http.SameSiteNoneMode:
			c.SameSite = "None"
		}
		if c.Path == "" || c.Path[0] != '/' {
			c.Path = defaultPath(u)
		}
		domain := strings.TrimPrefix(strings.ToLower(hc.Domain), ".")
		if domain == "" {
			c.Domain, c.HostOnly = host, true
		} else {
			if !domainMatch(host, domain) {
				continue
			}
			if ps, _ := publicsuffix.PublicSuffix(domain); ps == domain && host != domain {
				continue
			}
			c.Domain = domain
		}
		if hc.MaxAge < 0 {
			c.Expires = now.Add(-time.Second)
		} else if hc.MaxAge > 0 {
			c.Expires = now.Add(time.Duration(hc.MaxAge) * time.Second)
		} else if !hc.Expires.IsZero() {
			c.Expires = hc.Expires
		}
		if c.expired(now) {
			delete(j.cookies, c.key())
		} else {
			j.cookies[c.key()] = c
		}
		changed = true
	}
	cb := j.OnChange
	j.mu.Unlock()
	if changed && cb != nil {
		cb()
	}
}

// Cookies implements http.CookieJar.
func (j *Jar) Cookies(u *url.URL) []*http.Cookie {
	list := j.Match(u)
	out := make([]*http.Cookie, len(list))
	for i, c := range list {
		out[i] = &http.Cookie{Name: c.Name, Value: c.Value}
	}
	return out
}

// Match returns the cookies that would be sent to u.
func (j *Jar) Match(u *url.URL) []Cookie {
	host := canonicalHost(u)
	secure := u.Scheme == "https" || u.Scheme == "wss"
	path := u.EscapedPath()
	now := time.Now()
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []Cookie
	for k, c := range j.cookies {
		if c.expired(now) {
			delete(j.cookies, k)
			continue
		}
		if c.HostOnly && host != c.Domain || !c.HostOnly && !domainMatch(host, c.Domain) {
			continue
		}
		if !pathMatch(path, c.Path) || c.Secure && !secure {
			continue
		}
		out = append(out, *c)
	}
	sort.Slice(out, func(a, b int) bool { return len(out[a].Path) > len(out[b].Path) })
	return out
}

// All returns every stored cookie, sorted by domain then name.
func (j *Jar) All() []Cookie {
	now := time.Now()
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]Cookie, 0, len(j.cookies))
	for k, c := range j.cookies {
		if c.expired(now) {
			delete(j.cookies, k)
			continue
		}
		out = append(out, *c)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Domain != out[b].Domain {
			return out[a].Domain < out[b].Domain
		}
		return out[a].Name < out[b].Name
	})
	return out
}

// Put stores a cookie directly.
func (j *Jar) Put(c Cookie) {
	if c.Path == "" {
		c.Path = "/"
	}
	c.Domain = strings.TrimPrefix(strings.ToLower(c.Domain), ".")
	j.mu.Lock()
	j.cookies[c.key()] = &c
	cb := j.OnChange
	j.mu.Unlock()
	if cb != nil {
		cb()
	}
}

// Delete removes cookies. Empty path/name act as wildcards.
func (j *Jar) Delete(domain, path, name string) {
	domain = strings.TrimPrefix(strings.ToLower(domain), ".")
	j.mu.Lock()
	for k, c := range j.cookies {
		if (domain == "" || c.Domain == domain) && (path == "" || c.Path == path) && (name == "" || c.Name == name) {
			delete(j.cookies, k)
		}
	}
	cb := j.OnChange
	j.mu.Unlock()
	if cb != nil {
		cb()
	}
}

// Load reads cookies from a JSON file. A missing file is not an error.
func (j *Jar) Load(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var list []Cookie
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	for i := range list {
		c := list[i]
		j.cookies[c.key()] = &c
	}
	return nil
}

// Save writes persistent (non-session) and session cookies to a JSON file.
func (j *Jar) Save(path string) error {
	data, err := json.MarshalIndent(j.All(), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
