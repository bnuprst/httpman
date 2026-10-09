package script

import (
	"context"
	"encoding/json"
	"testing"
)

func benchInput() (Input, []Source) {
	return Input{Event: "test", Request: json.RawMessage(`{"url":"http://x"}`), Response: jsonResponse(200, `{"a":1}`)},
		[]Source{{Name: "t", Code: `pm.test("ok", () => pm.expect(pm.response.json().a).to.equal(1));`}}
}

func BenchmarkFreshVM(b *testing.B) {
	in, src := benchInput()
	for i := 0; i < b.N; i++ {
		if _, err := Run(context.Background(), nil, in, src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReusedVM(b *testing.B) {
	in, src := benchInput()
	vm, err := NewVM()
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := vm.Run(context.Background(), nil, in, src)
		if err != nil || len(out.Tests) != 1 || !out.Tests[0].Passed {
			b.Fatal(err, out)
		}
	}
}
