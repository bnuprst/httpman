package httpclient

import (
	"crypto/hmac"
	"crypto/md5" //nolint:gosec // required by digest auth
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // required by oauth1
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bnuprst/httpman/internal/collection"
)

type authState struct {
	digest *digestAuth
}

func applyAuth(a *collection.Auth, method string, u *url.URL, h http.Header, body []byte, warnings *[]string) (authState, error) {
	var st authState
	if a == nil {
		return st, nil
	}
	switch a.Type {
	case "", "noauth", "inherit":
	case "basic":
		if h.Get("Authorization") == "" {
			cred := a.Get("username") + ":" + a.Get("password")
			h.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(cred)))
		}
	case "bearer":
		if tok := a.Get("token"); tok != "" && h.Get("Authorization") == "" {
			h.Set("Authorization", "Bearer "+tok)
		}
	case "apikey":
		key, val := a.Get("key"), a.Get("value")
		if key == "" {
			break
		}
		if a.Get("in") == "query" {
			addQuery(u, key, val)
		} else if h.Get(key) == "" {
			h.Set(key, val)
		}
	case "oauth2":
		tok := a.Get("accessToken")
		if tok == "" {
			break
		}
		if a.Get("addTokenTo") == "queryParams" {
			addQuery(u, "access_token", tok)
		} else if h.Get("Authorization") == "" {
			prefix := a.Get("headerPrefix")
			if prefix == "" {
				prefix = "Bearer"
			}
			h.Set("Authorization", strings.TrimSpace(prefix+" "+tok))
		}
	case "digest":
		st.digest = &digestAuth{username: a.Get("username"), password: a.Get("password")}
		// If the challenge parameters are known up front, authorize immediately.
		if realm, nonce := a.Get("realm"), a.Get("nonce"); realm != "" && nonce != "" {
			chal := fmt.Sprintf(`Digest realm="%s", nonce="%s", qop="%s", algorithm=%s, opaque="%s"`,
				realm, nonce, a.Get("qop"), defaultStr(a.Get("algorithm"), "MD5"), a.Get("opaque"))
			if v, err := st.digest.authorize(chal, method, u, body); err == nil {
				h.Set("Authorization", v)
			}
		}
	case "oauth1":
		if err := signOAuth1(a, method, u, h, body); err != nil {
			return st, err
		}
	case "awsv4":
		signAWSv4(a, method, u, h, body)
	default:
		*warnings = append(*warnings, fmt.Sprintf("auth type %q is not supported; request sent without it", a.Type))
	}
	return st, nil
}

func defaultStr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func addQuery(u *url.URL, k, v string) {
	if u.RawQuery != "" {
		u.RawQuery += "&"
	}
	u.RawQuery += url.QueryEscape(k) + "=" + url.QueryEscape(v)
}

// ---- Digest ----

type digestAuth struct {
	username, password string
	nc                 int
}

func parseChallenge(s string) map[string]string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, ' '); i >= 0 {
		s = s[i+1:]
	}
	out := map[string]string{}
	for len(s) > 0 {
		s = strings.TrimLeft(s, " ,")
		eq := strings.IndexByte(s, '=')
		if eq < 0 {
			break
		}
		key := strings.ToLower(strings.TrimSpace(s[:eq]))
		s = s[eq+1:]
		var val string
		if strings.HasPrefix(s, `"`) {
			end := strings.IndexByte(s[1:], '"')
			if end < 0 {
				val, s = s[1:], ""
			} else {
				val, s = s[1:end+1], s[end+2:]
			}
		} else {
			end := strings.IndexByte(s, ',')
			if end < 0 {
				val, s = s, ""
			} else {
				val, s = s[:end], s[end:]
			}
		}
		out[key] = strings.TrimSpace(val)
	}
	return out
}

func (d *digestAuth) authorize(challenge, method string, u *url.URL, body []byte) (string, error) {
	c := parseChallenge(challenge)
	realm, nonce := c["realm"], c["nonce"]
	if nonce == "" {
		return "", fmt.Errorf("challenge has no nonce")
	}
	algo := strings.ToUpper(defaultStr(c["algorithm"], "MD5"))
	var hf func() hash.Hash
	switch strings.TrimSuffix(algo, "-SESS") {
	case "MD5":
		hf = md5.New
	case "SHA-256":
		hf = sha256.New
	case "SHA-512-256":
		hf = sha512.New512_256
	default:
		return "", fmt.Errorf("unsupported algorithm %s", algo)
	}
	H := func(s string) string {
		h := hf()
		h.Write([]byte(s))
		return hex.EncodeToString(h.Sum(nil))
	}
	d.nc++
	nc := fmt.Sprintf("%08x", d.nc)
	cnonce := randomHex(8)
	uri := u.RequestURI()
	ha1 := H(d.username + ":" + realm + ":" + d.password)
	if strings.HasSuffix(algo, "-SESS") {
		ha1 = H(ha1 + ":" + nonce + ":" + cnonce)
	}
	qop := ""
	for _, q := range strings.Split(c["qop"], ",") {
		q = strings.TrimSpace(q)
		if q == "auth" || (q == "auth-int" && qop == "") {
			qop = q
		}
	}
	ha2 := H(method + ":" + uri)
	if qop == "auth-int" {
		ha2 = H(method + ":" + uri + ":" + H(string(body)))
	}
	var resp string
	if qop == "" {
		resp = H(ha1 + ":" + nonce + ":" + ha2)
	} else {
		resp = H(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
	}
	parts := []string{
		fmt.Sprintf(`username="%s"`, d.username),
		fmt.Sprintf(`realm="%s"`, realm),
		fmt.Sprintf(`nonce="%s"`, nonce),
		fmt.Sprintf(`uri="%s"`, uri),
		"algorithm=" + algo,
		fmt.Sprintf(`response="%s"`, resp),
	}
	if qop != "" {
		parts = append(parts, "qop="+qop, "nc="+nc, fmt.Sprintf(`cnonce="%s"`, cnonce))
	}
	if op := c["opaque"]; op != "" {
		parts = append(parts, fmt.Sprintf(`opaque="%s"`, op))
	}
	return "Digest " + strings.Join(parts, ", "), nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---- OAuth 1.0 ----

func oauthEscape(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_' || c == '~' {
			sb.WriteByte(c)
		} else {
			fmt.Fprintf(&sb, "%%%02X", c)
		}
	}
	return sb.String()
}

func signOAuth1(a *collection.Auth, method string, u *url.URL, h http.Header, body []byte) error {
	params := map[string]string{
		"oauth_consumer_key":     a.Get("consumerKey"),
		"oauth_signature_method": defaultStr(a.Get("signatureMethod"), "HMAC-SHA1"),
		"oauth_timestamp":        defaultStr(a.Get("timestamp"), strconv.FormatInt(time.Now().Unix(), 10)),
		"oauth_nonce":            defaultStr(a.Get("nonce"), randomHex(16)),
		"oauth_version":          defaultStr(a.Get("version"), "1.0"),
	}
	if tok := a.Get("token"); tok != "" {
		params["oauth_token"] = tok
	}
	if cb := a.Get("callback"); cb != "" {
		params["oauth_callback"] = cb
	}
	if v := a.Get("verifier"); v != "" {
		params["oauth_verifier"] = v
	}
	type pair struct{ k, v string }
	var all []pair
	for k, v := range params {
		all = append(all, pair{oauthEscape(k), oauthEscape(v)})
	}
	for k, vs := range u.Query() {
		for _, v := range vs {
			all = append(all, pair{oauthEscape(k), oauthEscape(v)})
		}
	}
	if strings.HasPrefix(h.Get("Content-Type"), "application/x-www-form-urlencoded") {
		if form, err := url.ParseQuery(string(body)); err == nil {
			for k, vs := range form {
				for _, v := range vs {
					all = append(all, pair{oauthEscape(k), oauthEscape(v)})
				}
			}
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].k != all[j].k {
			return all[i].k < all[j].k
		}
		return all[i].v < all[j].v
	})
	var ps []string
	for _, p := range all {
		ps = append(ps, p.k+"="+p.v)
	}
	baseURL := strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + u.EscapedPath()
	base := strings.ToUpper(method) + "&" + oauthEscape(baseURL) + "&" + oauthEscape(strings.Join(ps, "&"))
	key := oauthEscape(a.Get("consumerSecret")) + "&" + oauthEscape(a.Get("tokenSecret"))
	var sig string
	switch params["oauth_signature_method"] {
	case "HMAC-SHA1":
		m := hmac.New(sha1.New, []byte(key))
		m.Write([]byte(base))
		sig = base64.StdEncoding.EncodeToString(m.Sum(nil))
	case "HMAC-SHA256":
		m := hmac.New(sha256.New, []byte(key))
		m.Write([]byte(base))
		sig = base64.StdEncoding.EncodeToString(m.Sum(nil))
	case "HMAC-SHA512":
		m := hmac.New(sha512.New, []byte(key))
		m.Write([]byte(base))
		sig = base64.StdEncoding.EncodeToString(m.Sum(nil))
	case "PLAINTEXT":
		sig = key
	default:
		return fmt.Errorf("oauth1: unsupported signature method %q", params["oauth_signature_method"])
	}
	params["oauth_signature"] = sig
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if a.Get("addParamsToHeader") == "false" {
		for _, k := range keys {
			addQuery(u, k, params[k])
		}
		return nil
	}
	var hp []string
	if realm := a.Get("realm"); realm != "" {
		hp = append(hp, fmt.Sprintf(`realm="%s"`, oauthEscape(realm)))
	}
	for _, k := range keys {
		hp = append(hp, fmt.Sprintf(`%s="%s"`, k, oauthEscape(params[k])))
	}
	h.Set("Authorization", "OAuth "+strings.Join(hp, ","))
	return nil
}

// ---- AWS Signature V4 ----

func signAWSv4(a *collection.Auth, method string, u *url.URL, h http.Header, body []byte) {
	region := defaultStr(a.Get("region"), "us-east-1")
	service := defaultStr(a.Get("service"), "execute-api")
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	date := now.Format("20060102")
	sum := sha256.Sum256(body)
	payloadHash := hex.EncodeToString(sum[:])
	h.Set("X-Amz-Date", amzDate)
	h.Set("X-Amz-Content-Sha256", payloadHash)
	if tok := a.Get("sessionToken"); tok != "" {
		h.Set("X-Amz-Security-Token", tok)
	}
	signed := map[string]string{"host": u.Host}
	for k, vs := range h {
		lk := strings.ToLower(k)
		if lk == "user-agent" || lk == "connection" || lk == "accept-encoding" {
			continue
		}
		signed[lk] = strings.Join(vs, ",")
	}
	names := make([]string, 0, len(signed))
	for k := range signed {
		names = append(names, k)
	}
	sort.Strings(names)
	var ch strings.Builder
	for _, k := range names {
		ch.WriteString(k + ":" + strings.TrimSpace(signed[k]) + "\n")
	}
	q := u.Query()
	qk := make([]string, 0, len(q))
	for k := range q {
		qk = append(qk, k)
	}
	sort.Strings(qk)
	var cq []string
	for _, k := range qk {
		vs := append([]string{}, q[k]...)
		sort.Strings(vs)
		for _, v := range vs {
			cq = append(cq, oauthEscape(k)+"="+oauthEscape(v))
		}
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	canonical := strings.Join([]string{method, path, strings.Join(cq, "&"), ch.String(), strings.Join(names, ";"), payloadHash}, "\n")
	cr := sha256.Sum256([]byte(canonical))
	scope := date + "/" + region + "/" + service + "/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(cr[:])
	mac := func(key []byte, data string) []byte {
		m := hmac.New(sha256.New, key)
		m.Write([]byte(data))
		return m.Sum(nil)
	}
	k := mac([]byte("AWS4"+a.Get("secretKey")), date)
	k = mac(k, region)
	k = mac(k, service)
	k = mac(k, "aws4_request")
	sig := hex.EncodeToString(mac(k, toSign))
	h.Set("Authorization", fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		a.Get("accessKey"), scope, strings.Join(names, ";"), sig))
}
