package cli

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/bnuprst/httpman/internal/runner"
)

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0 && enableVT(f)
}

type cliReporter struct {
	w          io.Writer
	color      bool
	verbose    bool
	unicode    bool
	lastPath   string
	failures   []string
	pending    []string
	iterations int
}

func newCLIReporter(w io.Writer, color, verbose, unicode bool) *cliReporter {
	return &cliReporter{w: w, color: color, verbose: verbose, unicode: unicode}
}

func (r *cliReporter) c(code, s string) string {
	if !r.color {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (r *cliReporter) sym(u, a string) string {
	if r.unicode {
		return u
	}
	return a
}

func (r *cliReporter) header(name string) {
	fmt.Fprintf(r.w, "httpman\n\n%s\n", r.c("1", name))
}

func (r *cliReporter) console(e runner.ConsoleEntry) {
	switch e.Level {
	case "request":
		if r.verbose && e.Data != nil {
			r.pending = append(r.pending, fmt.Sprintf("  %s\n", r.c("2", e.Message)))
		}
	case "error", "warn":
		r.pending = append(r.pending, fmt.Sprintf("  %s %s\n", r.c("33", r.sym("┃", "|")), r.c("33", e.Message)))
	default:
		for _, line := range strings.Split(e.Message, "\n") {
			r.pending = append(r.pending, fmt.Sprintf("  %s %s\n", r.c("2", r.sym("┊", ":")), line))
		}
	}
}

func size(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%dB", n)
}

func (r *cliReporter) event(ev runner.RunEvent) {
	switch ev.Type {
	case "iteration":
		r.iterations++
		r.lastPath = ""
		if ev.Iteration > 0 {
			fmt.Fprintf(r.w, "\nIteration %d\n", ev.Iteration+1)
		}
	case "execution":
		ex := ev.Execution
		path := strings.Join(ex.Path, " / ")
		if path != "" && path != r.lastPath {
			fmt.Fprintf(r.w, "\n%s %s\n", r.sym("❏", "#"), path)
		}
		r.lastPath = path
		fmt.Fprintf(r.w, "%s %s\n", r.sym("↳", "->"), ex.Name)
		if ex.Skipped {
			fmt.Fprintf(r.w, "  %s\n", r.c("2", "(request skipped)"))
		} else if ex.Request != nil {
			line := fmt.Sprintf("  %s %s", ex.Request.Method, ex.Request.URL)
			if ex.Response != nil {
				status := fmt.Sprintf("%d %s", ex.Response.Code, ex.Response.Status)
				code := "32"
				if ex.Response.Code >= 400 {
					code = "31"
				}
				line += fmt.Sprintf(" [%s, %s, %.0fms]", r.c(code, status), size(ex.Response.BodySize), ex.Response.Time)
			}
			fmt.Fprintln(r.w, line)
		}
		for _, p := range r.pending {
			fmt.Fprint(r.w, p)
		}
		r.pending = nil
		if ex.Error != "" {
			r.failures = append(r.failures, fmt.Sprintf("%s\n    %s", ex.Name, ex.Error))
			fmt.Fprintf(r.w, "  %s\n", r.c("31", fmt.Sprintf("%d. %s", len(r.failures), ex.Error)))
		}
		for _, e := range ex.ScriptErrors {
			r.failures = append(r.failures, fmt.Sprintf("%s\n    %s", ex.Name, e.String()))
			fmt.Fprintf(r.w, "  %s\n", r.c("31", fmt.Sprintf("%d. %s", len(r.failures), e.String())))
		}
		for _, t := range ex.Tests {
			switch {
			case t.Skipped:
				fmt.Fprintf(r.w, "  %s %s\n", r.c("2", "-"), r.c("2", t.Name))
			case t.Passed:
				fmt.Fprintf(r.w, "  %s %s\n", r.c("32", r.sym("✓", "ok")), r.c("2", t.Name))
			default:
				msg := ""
				if t.Error != nil {
					msg = t.Error.Name + ": " + t.Error.Message
				}
				r.failures = append(r.failures, fmt.Sprintf("%s\n    %s\n    %s", ex.Name, t.Name, msg))
				fmt.Fprintf(r.w, "  %s\n", r.c("31", fmt.Sprintf("%d. %s", len(r.failures), t.Name)))
			}
		}
	}
}

func (r *cliReporter) summary(s *runner.Summary) {
	b := func(u, a string) string { return r.sym(u, a) }
	h, v := b("─", "-"), b("│", "|")
	rows := [][3]string{
		{"iterations", fmt.Sprint(s.Iterations), "0"},
		{"requests", fmt.Sprint(s.Requests), fmt.Sprint(s.FailedReqs)},
		{"test-scripts errors", "", fmt.Sprint(s.ScriptErrors)},
		{"assertions", fmt.Sprint(s.Tests - s.TestsSkipped), fmt.Sprint(s.TestsFailed)},
	}
	w1, w2, w3 := 28, 20, 20
	line := func(l, m, rr string) string {
		return l + strings.Repeat(h, w1) + m + strings.Repeat(h, w2) + m + strings.Repeat(h, w3) + rr
	}
	fmt.Fprintln(r.w)
	fmt.Fprintln(r.w, line(b("┌", "+"), b("┬", "+"), b("┐", "+")))
	fmt.Fprintf(r.w, "%s%*s %s%*s %s%*s %s\n", v, w1-1, "", v, w2-1, "executed", v, w3-1, "failed", v)
	fmt.Fprintln(r.w, line(b("├", "+"), b("┼", "+"), b("┤", "+")))
	for _, row := range rows {
		failed := row[2]
		if failed != "0" {
			failed = r.c("31", fmt.Sprintf("%*s", w3-1, failed))
		} else {
			failed = fmt.Sprintf("%*s", w3-1, failed)
		}
		fmt.Fprintf(r.w, "%s%*s %s%*s %s%s %s\n", v, w1-1, row[0], v, w2-1, row[1], v, failed, v)
	}
	fmt.Fprintln(r.w, line(b("├", "+"), b("┴", "+"), b("┤", "+")))
	info := fmt.Sprintf(" total run duration: %s, average response time: %.0fms", s.Duration.Round(time.Millisecond), s.AvgResponse)
	fmt.Fprintf(r.w, "%s%-*s%s\n", v, w1+w2+w3+2, info, v)
	fmt.Fprintln(r.w, line(b("└", "+"), b("─", "-"), b("┘", "+")))
	if s.Stopped {
		fmt.Fprintln(r.w, r.c("33", "run stopped early"))
	}
	if len(r.failures) > 0 {
		fmt.Fprintf(r.w, "\n  #  failure\n\n")
		for i, f := range r.failures {
			fmt.Fprintf(r.w, "%3d. %s\n\n", i+1, f)
		}
	}
}

// ---- JSON ----

func jsonReport(s *runner.Summary) any {
	type exec struct {
		Name         string   `json:"name"`
		Path         []string `json:"path,omitempty"`
		Iteration    int      `json:"iteration"`
		Method       string   `json:"method,omitempty"`
		URL          string   `json:"url,omitempty"`
		Code         int      `json:"code,omitempty"`
		Status       string   `json:"status,omitempty"`
		Time         float64  `json:"responseTime,omitempty"`
		Size         int64    `json:"responseSize,omitempty"`
		Error        string   `json:"error,omitempty"`
		Skipped      bool     `json:"skipped,omitempty"`
		Tests        any      `json:"assertions"`
		ScriptErrors any      `json:"scriptErrors,omitempty"`
	}
	var execs []exec
	for _, ex := range s.Executions {
		e := exec{Name: ex.Name, Path: ex.Path, Iteration: ex.Iteration, Error: ex.Error, Skipped: ex.Skipped, Tests: ex.Tests}
		if len(ex.ScriptErrors) > 0 {
			e.ScriptErrors = ex.ScriptErrors
		}
		if ex.Request != nil {
			e.Method, e.URL = ex.Request.Method, ex.Request.URL
		}
		if ex.Response != nil {
			e.Code, e.Status, e.Time, e.Size = ex.Response.Code, ex.Response.Status, ex.Response.Time, ex.Response.BodySize
		}
		execs = append(execs, e)
	}
	return map[string]any{
		"collection":  s.Collection,
		"environment": s.Environment,
		"started":     s.Started,
		"duration":    s.Duration.Milliseconds(),
		"stats": map[string]any{
			"iterations":      s.Iterations,
			"requests":        s.Requests,
			"failedRequests":  s.FailedReqs,
			"assertions":      s.Tests,
			"passed":          s.TestsPassed,
			"failed":          s.TestsFailed,
			"skipped":         s.TestsSkipped,
			"scriptErrors":    s.ScriptErrors,
			"avgResponseTime": s.AvgResponse,
		},
		"stopped":    s.Stopped,
		"executions": execs,
	}
}

// ---- JUnit ----

type junitFailure struct {
	Type    string `xml:"type,attr"`
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	ClassName string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	Error     *junitFailure `xml:"error,omitempty"`
	Skipped   *struct{}     `xml:"skipped,omitempty"`
}

type junitSuite struct {
	Name      string      `xml:"name,attr"`
	ID        string      `xml:"id,attr"`
	Timestamp string      `xml:"timestamp,attr"`
	Tests     int         `xml:"tests,attr"`
	Failures  int         `xml:"failures,attr"`
	Errors    int         `xml:"errors,attr"`
	Skipped   int         `xml:"skipped,attr"`
	Time      string      `xml:"time,attr"`
	Cases     []junitCase `xml:"testcase"`
}

type junitSuites struct {
	XMLName  xml.Name     `xml:"testsuites"`
	Name     string       `xml:"name,attr"`
	Tests    int          `xml:"tests,attr"`
	Failures int          `xml:"failures,attr"`
	Time     string       `xml:"time,attr"`
	Suites   []junitSuite `xml:"testsuite"`
}

func junitReport(s *runner.Summary) []byte {
	root := junitSuites{Name: s.Collection, Time: fmt.Sprintf("%.3f", s.Duration.Seconds())}
	for i, ex := range s.Executions {
		name := strings.Join(append(append([]string{}, ex.Path...), ex.Name), " / ")
		secs := 0.0
		if ex.Response != nil {
			secs = ex.Response.Time / 1000
		}
		suite := junitSuite{Name: name, ID: fmt.Sprint(i), Timestamp: s.Started.Format(time.RFC3339), Time: fmt.Sprintf("%.3f", secs)}
		class := strings.ReplaceAll(name, " / ", ".")
		if ex.Error != "" {
			suite.Errors++
			suite.Cases = append(suite.Cases, junitCase{Name: "request", ClassName: class, Time: suite.Time, Error: &junitFailure{Type: "RequestError", Message: ex.Error, Text: ex.Error}})
		}
		for _, e := range ex.ScriptErrors {
			suite.Errors++
			suite.Cases = append(suite.Cases, junitCase{Name: "script", ClassName: class, Time: suite.Time, Error: &junitFailure{Type: e.Name, Message: e.Message, Text: e.String()}})
		}
		for _, t := range ex.Tests {
			c := junitCase{Name: t.Name, ClassName: class, Time: suite.Time}
			switch {
			case t.Skipped:
				c.Skipped = &struct{}{}
				suite.Skipped++
			case !t.Passed:
				msg := ""
				typ := "AssertionError"
				if t.Error != nil {
					msg, typ = t.Error.Message, t.Error.Name
				}
				c.Failure = &junitFailure{Type: typ, Message: msg, Text: msg}
				suite.Failures++
			}
			suite.Cases = append(suite.Cases, c)
		}
		suite.Tests = len(suite.Cases)
		root.Tests += suite.Tests
		root.Failures += suite.Failures + suite.Errors
		root.Suites = append(root.Suites, suite)
	}
	out, _ := xml.MarshalIndent(root, "", "  ")
	return append([]byte(xml.Header), out...)
}
