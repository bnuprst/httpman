package runner

import (
	"testing"

	"github.com/bnuprst/httpman/internal/collection"
)

// Regression tests for behaviors found by running newman's integration
// fixtures.

func TestCSVParsingLikeNewman(t *testing.T) {
	d, err := ParseData([]byte("\xef\xbb\xbfname, num ,quoted,neg,empty,long,ws,unescaped\n"+
		`"Kane, A",7890,"00123",-7890,,"89111702272002019559", "foo" ,Abhijit "KDOS" Kane`+"\nx\n"), false)
	if err != nil {
		t.Fatal(err)
	}
	r := d.Rows[0]
	want := map[string]any{"name": "Kane, A", "num": float64(7890), "quoted": "00123", "neg": float64(-7890), "empty": "", "long": "89111702272002019559", "ws": "foo", "unescaped": `Abhijit "KDOS" Kane`}
	for k, v := range want {
		if r[k] != v {
			t.Errorf("%s = %#v, want %#v", k, r[k], v)
		}
	}
	if _, ok := d.Rows[1]["num"]; ok || d.Rows[1]["name"] != "x" {
		t.Errorf("short row: %#v", d.Rows[1])
	}
}

func TestURLFromParts(t *testing.T) {
	c, err := collection.Parse([]byte(`{"name":"bare","item":[
		{"name":"a","request":{"url":{"raw":"  ","protocol":"https","host":["example","com"],"path":["get"],"query":[{"key":null,"value":null},{"key":"foo","value":null},{"key":"x","value":""}]}}},
		{"name":"b","request":{"url":"https://example.com/plain?q=1"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Info.Name != "bare" {
		t.Errorf("name %q", c.Info.Name)
	}
	if got := c.Item[0].Request.URL.String(); got != "https://example.com/get?&foo&x=" {
		t.Errorf("parts: %s", got)
	}
	if got := c.Item[1].Request.URL.String(); got != "https://example.com/plain?q=1" {
		t.Errorf("string url: %s", got)
	}
}
