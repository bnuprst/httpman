// Package script runs Postman pre-request and test scripts in an embedded
// JavaScript engine (goja) that exposes the pm.* API.
package script

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"

	"github.com/bnuprst/httpman/internal/vars"
)

// The bundle is generated from ../../sandbox by `npm run build`.
//
//go:embed sandbox.bundle.js
var bundleSource string

var (
	programOnce sync.Once
	program     *goja.Program
	programErr  error
)

func compiled() (*goja.Program, error) {
	programOnce.Do(func() {
		program, programErr = goja.Compile("sandbox.js", bundleSource, false)
	})
	return program, programErr
}

// Host provides side effects to scripts.
type Host interface {
	Log(level, message string)
	// Send performs pm.sendRequest. The request is a Postman request JSON
	// with variables already resolved; the result is a response JSON.
	Send(ctx context.Context, request json.RawMessage) (json.RawMessage, error)
	// Cookies implements pm.cookies.jar() operations.
	Cookies(op string, args json.RawMessage) (any, error)
}

// Info is exposed as pm.info.
type Info struct {
	EventName      string   `json:"eventName"`
	Iteration      int      `json:"iteration"`
	IterationCount int      `json:"iterationCount"`
	RequestName    string   `json:"requestName"`
	RequestID      string   `json:"requestId"`
	Location       []string `json:"location,omitempty"`
}

// Scopes are the variable scopes visible to the script.
type Scopes struct {
	Globals             []vars.Var `json:"globals"`
	CollectionVariables []vars.Var `json:"collectionVariables"`
	Environment         []vars.Var `json:"environment"`
	EnvironmentName     string     `json:"environmentName,omitempty"`
	IterationData       []vars.Var `json:"iterationData,omitempty"`
	Local               []vars.Var `json:"_variables"`
}

// ResponseData is the response exposed as pm.response.
type ResponseData struct {
	Code         int             `json:"code"`
	Status       string          `json:"status"`
	Header       any             `json:"header"`
	Body         string          `json:"body"`
	ResponseTime float64         `json:"responseTime"`
	ResponseSize int64           `json:"responseSize"`
	Cookies      any             `json:"cookies,omitempty"`
}

// Input is everything a script execution needs.
type Input struct {
	Event          string          `json:"event"`
	Info           Info            `json:"info"`
	CollectionName string          `json:"collectionName,omitempty"`
	Scopes         Scopes          `json:"scopes"`
	Request        json.RawMessage `json:"request"`
	Response       *ResponseData   `json:"response,omitempty"`
	Cookies        any             `json:"cookies,omitempty"`
}

// Source is one script to run (collection, folder or request level).
type Source struct {
	Name string // shown in error locations, e.g. "collection" or the request name
	Code string
}

// TestResult is the outcome of one pm.test or tests[] entry.
type TestResult struct {
	Name    string     `json:"name"`
	Passed  bool       `json:"passed"`
	Skipped bool       `json:"skipped"`
	Error   *ErrorInfo `json:"error,omitempty"`
}

// ErrorInfo describes a JavaScript error.
type ErrorInfo struct {
	Name    string `json:"name"`
	Message string `json:"message"`
	Stack   string `json:"stack,omitempty"`
	Source  string `json:"source,omitempty"`
}

func (e ErrorInfo) String() string {
	if e.Source != "" {
		return fmt.Sprintf("%s: %s (in %s)", e.Name, e.Message, e.Source)
	}
	return e.Name + ": " + e.Message
}

// NextRequest captures postman.setNextRequest / pm.execution.setNextRequest.
// Value is nil when the run should stop.
type NextRequest struct {
	Value *string `json:"value"`
}

// Output is the result of running scripts.
type Output struct {
	Scopes struct {
		Globals             []vars.Var `json:"globals"`
		CollectionVariables []vars.Var `json:"collectionVariables"`
		Environment         []vars.Var `json:"environment"`
		Local               []vars.Var `json:"_variables"`
	} `json:"scopes"`
	Request     json.RawMessage `json:"request"`
	Tests       []TestResult    `json:"tests"`
	Errors      []ErrorInfo     `json:"errors"`
	NextRequest *NextRequest    `json:"nextRequest,omitempty"`
	SkipRequest bool            `json:"skipRequest"`
	Visualizer  json.RawMessage `json:"visualizer,omitempty"`
}

// DefaultTimeout bounds a single event (all scripts of a prerequest or test phase).
var DefaultTimeout = 60 * time.Second

// Run executes scripts in order within one JS context and returns the
// resulting state. A returned error means the sandbox itself failed;
// errors thrown by user scripts are reported in Output.Errors.
func Run(ctx context.Context, host Host, in Input, sources []Source) (*Output, error) {
	prog, err := compiled()
	if err != nil {
		return nil, fmt.Errorf("sandbox bundle: %w", err)
	}
	if _, ok := ctx.Deadline(); !ok && DefaultTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultTimeout)
		defer cancel()
	}

	vm := goja.New()
	vm.SetMaxCallStackSize(5000)
	stop := context.AfterFunc(ctx, func() { vm.Interrupt(ctx.Err()) })
	defer stop()

	installHost(ctx, vm, host)
	if _, err := vm.RunProgram(prog); err != nil {
		return nil, fmt.Errorf("sandbox init: %w", err)
	}
	stateJSON, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	setup, _ := goja.AssertFunction(vm.Get("__setup"))
	if _, err := setup(goja.Undefined(), vm.ToValue(string(stateJSON))); err != nil {
		return nil, fmt.Errorf("sandbox setup: %w", err)
	}
	scriptError, _ := goja.AssertFunction(vm.Get("__scriptError"))

	var extra []ErrorInfo
	for _, src := range sources {
		if strings.TrimSpace(src.Code) == "" {
			continue
		}
		// Postman wraps scripts in a function, so top-level `return` and
		// `await` are allowed. Keep the first line on the wrapper line so
		// reported line numbers match the editor.
		code := "(async function(){" + src.Code + "\n})().catch(function(e){__scriptError(e)})"
		_, err := vm.RunScript(src.Name, code)
		if err != nil {
			if ie := (*goja.InterruptedError)(nil); errors.As(err, &ie) {
				extra = append(extra, ErrorInfo{Name: "TimeoutError", Message: "script execution timed out", Source: src.Name})
				break
			}
			extra = append(extra, jsError(err, src.Name))
			continue
		}
		if err := drain(ctx, vm); err != nil {
			extra = append(extra, ErrorInfo{Name: "TimeoutError", Message: "script execution timed out", Source: src.Name})
			break
		}
	}
	_ = scriptError

	vm.ClearInterrupt()
	finish, _ := goja.AssertFunction(vm.Get("__finish"))
	res, err := finish(goja.Undefined())
	if err != nil {
		return nil, fmt.Errorf("sandbox finish: %w", err)
	}
	out := &Output{}
	if err := json.Unmarshal([]byte(res.String()), out); err != nil {
		return nil, fmt.Errorf("sandbox result: %w", err)
	}
	out.Errors = append(out.Errors, extra...)
	return out, nil
}

// drain runs pending timers (and thereby promise jobs) until none remain.
func drain(ctx context.Context, vm *goja.Runtime) (err error) {
	delayFn, _ := goja.AssertFunction(vm.Get("__timerDelay"))
	runFn, _ := goja.AssertFunction(vm.Get("__runTimer"))
	errFn, _ := goja.AssertFunction(vm.Get("__scriptError"))
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		v, err := delayFn(goja.Undefined())
		if err != nil {
			return err
		}
		d := v.ToInteger()
		if d < 0 {
			return nil
		}
		if d > 0 {
			t := time.NewTimer(time.Duration(d) * time.Millisecond)
			select {
			case <-ctx.Done():
				t.Stop()
				return ctx.Err()
			case <-t.C:
			}
		}
		if _, err := runFn(goja.Undefined()); err != nil {
			var ie *goja.InterruptedError
			if errors.As(err, &ie) {
				return err
			}
			var ex *goja.Exception
			if errors.As(err, &ex) {
				_, _ = errFn(goja.Undefined(), ex.Value())
				continue
			}
			return err
		}
	}
}

func jsError(err error, source string) ErrorInfo {
	var ex *goja.Exception
	if errors.As(err, &ex) {
		info := ErrorInfo{Name: "Error", Message: ex.Error(), Source: source}
		if obj, ok := ex.Value().(*goja.Object); ok && obj != nil {
			if n := obj.Get("name"); n != nil && !goja.IsUndefined(n) {
				info.Name = n.String()
			}
			if m := obj.Get("message"); m != nil && !goja.IsUndefined(m) {
				info.Message = m.String()
			}
		}
		return info
	}
	var ce *goja.CompilerSyntaxError
	if errors.As(err, &ce) {
		return ErrorInfo{Name: "SyntaxError", Message: ce.Error(), Source: source}
	}
	return ErrorInfo{Name: "Error", Message: err.Error(), Source: source}
}

func installHost(ctx context.Context, vm *goja.Runtime, host Host) {
	h := vm.NewObject()
	throw := func(err error) { panic(vm.NewGoError(err)) }
	_ = h.Set("log", func(level, msg string) {
		if host != nil {
			host.Log(level, msg)
		}
	})
	_ = h.Set("dynamic", func(name string) goja.Value {
		if v, ok := vars.Dynamic(name); ok {
			return vm.ToValue(v)
		}
		return goja.Undefined()
	})
	_ = h.Set("randomBytes", func(n int) goja.Value {
		if n < 0 || n > 65536 {
			throw(fmt.Errorf("randomBytes: invalid length %d", n))
		}
		b := make([]byte, n)
		_, _ = rand.Read(b)
		out := make([]any, n)
		for i, c := range b {
			out[i] = int(c)
		}
		return vm.NewArray(out...)
	})
	_ = h.Set("send", func(reqJSON string) string {
		if host == nil {
			return `{"error":"sendRequest is not available"}`
		}
		res, err := host.Send(ctx, json.RawMessage(reqJSON))
		if err != nil {
			b, _ := json.Marshal(map[string]string{"error": err.Error()})
			return string(b)
		}
		return string(res)
	})
	_ = h.Set("cookies", func(op, args string) string {
		if host == nil {
			return `{"error":"cookie jar is not available"}`
		}
		res, err := host.Cookies(op, json.RawMessage(args))
		var b []byte
		if err != nil {
			b, _ = json.Marshal(map[string]string{"error": err.Error()})
		} else {
			b, _ = json.Marshal(map[string]any{"result": res})
		}
		return string(b)
	})
	_ = vm.Set("__host", h)
}
