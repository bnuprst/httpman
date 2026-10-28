package collection

import "testing"

func TestStripJSONComments(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"none", `{"a": 1}`, `{"a": 1}`},
		{"commented field", "{\n  \"a\": 1,\n  // \"b\": 2,\n  \"c\": 3\n}", "{\n  \"a\": 1,\n  \"c\": 3\n}"},
		{"crlf", "{\r\n  //\"b\": 2,\r\n  \"c\": 3\r\n}", "{\r\n  \"c\": 3\r\n}"},
		{"trailing", "{\"a\": 1 // note\n}", "{\"a\": 1 \n}"},
		{"block", "{\n  /* \"b\": 2,\n  \"d\": 4, */\n  \"c\": 3\n}", "{\n  \"c\": 3\n}"},
		{"in string", `{"url": "http://x/*y*/", "q": "a\"//b"}`, `{"url": "http://x/*y*/", "q": "a\"//b"}`},
		{"blank lines kept", "{\n\n  \"a\": 1 /* x */\n}", "{\n\n  \"a\": 1 \n}"},
	}
	for _, c := range cases {
		if got := StripJSONComments(c.in); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestRawPayloadOnlyStripsJSON(t *testing.T) {
	b := &Body{Mode: "raw", Raw: "a // b", Options: []byte(`{"raw":{"language":"text"}}`)}
	if got := b.RawPayload(); got != "a // b" {
		t.Errorf("text body changed: %q", got)
	}
	b.Options = []byte(`{"raw":{"language":"json"}}`)
	if got := b.RawPayload(); got != "a " {
		t.Errorf("json body: %q", got)
	}
}
