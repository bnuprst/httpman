// Package cli implements the command-line interface: a newman-compatible
// collection runner plus workspace utilities.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bnuprst/httpman/internal/collection"
	"github.com/bnuprst/httpman/internal/cookies"
	"github.com/bnuprst/httpman/internal/httpclient"
	"github.com/bnuprst/httpman/internal/runner"
	"github.com/bnuprst/httpman/internal/script"
	"github.com/bnuprst/httpman/internal/vars"
	"github.com/bnuprst/httpman/internal/workspace"
)

// Version is set at build time.
var Version = "dev"

const usage = `httpman — a local HTTP client compatible with Postman collections

Usage:
  httpman                         start the desktop app
  httpman run <collection> [options]
                                  run a collection (newman-compatible flags)
  httpman import <file>...        import collections/environments into the workspace
  httpman list                    list collections and environments in the workspace
  httpman version                 print the version

<collection> is a path to a Postman collection file (v1, v2.0, v2.1) or the
id/name of a collection stored in the workspace.

Run options:
  -e, --environment <file|name>   environment file or workspace environment
  -g, --globals <file>            globals file (default: workspace globals)
  -d, --iteration-data <file>     CSV or JSON data file
  -n, --iteration-count <n>       number of iterations
  --folder <name|id>              run a single folder or request
  --env-var <key=value>           set an environment variable (repeatable)
  --global-var <key=value>        set a global variable (repeatable)
  --delay-request <ms>            delay between requests
  --timeout-request <ms>          request timeout
  --timeout-script <ms>           script timeout
  -k, --insecure                  disable TLS certificate verification
  --ignore-redirects              do not follow redirects
  --bail                          stop on the first failure
  --working-dir <dir>             base directory for file uploads
  --export-environment <file>     write the final environment
  --export-globals <file>         write the final globals
  --export-collection <file>      write the collection (with collection variables)
  -r, --reporters <list>          cli,json,junit (default cli)
  --reporter-json-export <file>   JSON report path (default stdout when only json)
  --reporter-junit-export <file>  JUnit XML path (default httpman-report.xml)
  --no-color                      disable colors
  --silent                        no cli output
  --verbose                       print request/response details
  --workspace <dir>               workspace directory (default ` + "%s" + `)
`

// IsCLI reports whether the arguments ask for a CLI command (vs. the GUI).
func IsCLI(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "run", "import", "list", "version", "--version", "-v", "help", "--help", "-h":
		return true
	}
	return false
}

// Main runs the CLI and returns the exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(stdout, usage, workspace.DefaultDir())
		return 0
	}
	var err error
	code := 0
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, "httpman", Version)
	case "help", "--help", "-h":
		fmt.Fprintf(stdout, usage, workspace.DefaultDir())
	case "run":
		code, err = runCmd(args[1:], stdout, stderr)
	case "import":
		err = importCmd(args[1:], stdout)
	case "list":
		err = listCmd(args[1:], stdout)
	default:
		err = fmt.Errorf("unknown command %q (see httpman help)", args[0])
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}

type options struct {
	positional []string
	values     map[string][]string
	flags      map[string]bool
}

var valueFlags = map[string]string{
	"-e": "environment", "--environment": "environment",
	"-g": "globals", "--globals": "globals",
	"-d": "iteration-data", "--iteration-data": "iteration-data",
	"-n": "iteration-count", "--iteration-count": "iteration-count",
	"--folder": "folder", "--env-var": "env-var", "--global-var": "global-var",
	"--delay-request": "delay-request", "--timeout-request": "timeout-request", "--timeout-script": "timeout-script",
	"--timeout":     "timeout",
	"--working-dir": "working-dir", "--export-environment": "export-environment", "--export-globals": "export-globals",
	"--export-collection": "export-collection",
	"-r":                  "reporters", "--reporters": "reporters", "--reporter-json-export": "reporter-json-export",
	"--reporter-junit-export": "reporter-junit-export", "--workspace": "workspace", "--color": "color",
}

var boolFlags = map[string]string{
	"-k": "insecure", "--insecure": "insecure", "--ignore-redirects": "ignore-redirects", "--bail": "bail",
	"--no-color": "no-color", "--silent": "silent", "--verbose": "verbose", "--disable-unicode": "disable-unicode",
	"--suppress-exit-code": "suppress-exit-code", "-x": "suppress-exit-code",
}

func parseArgs(args []string) (*options, error) {
	o := &options{values: map[string][]string{}, flags: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, val, hasVal := strings.Cut(a, "=")
		if !strings.HasPrefix(a, "-") || a == "-" {
			o.positional = append(o.positional, a)
			continue
		}
		if key, ok := valueFlags[name]; ok {
			if !hasVal {
				if i+1 >= len(args) {
					return nil, fmt.Errorf("flag %s needs a value", name)
				}
				i++
				val = args[i]
			}
			o.values[key] = append(o.values[key], val)
			continue
		}
		if key, ok := boolFlags[a]; ok {
			o.flags[key] = true
			continue
		}
		return nil, fmt.Errorf("unknown flag %s", a)
	}
	return o, nil
}

func (o *options) get(k string) string {
	if v := o.values[k]; len(v) > 0 {
		return v[len(v)-1]
	}
	return ""
}

func (o *options) intVal(k string) (int, error) {
	s := o.get(k)
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("--%s: %q is not a number", k, s)
	}
	return n, nil
}

func openWorkspace(o *options) (*workspace.Workspace, error) {
	dir := o.get("workspace")
	if dir == "" {
		dir = workspace.DefaultDir()
	}
	return workspace.Open(dir)
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func loadCollection(ws *workspace.Workspace, ref string) (*collection.Collection, string, error) {
	if isFile(ref) {
		data, err := os.ReadFile(ref)
		if err != nil {
			return nil, "", err
		}
		c, err := collection.Parse(data)
		return c, filepath.Dir(ref), err
	}
	metas, err := ws.Collections()
	if err != nil {
		return nil, "", err
	}
	for _, m := range metas {
		if m.ID == ref || strings.EqualFold(m.Name, ref) {
			c, err := ws.Collection(m.ID)
			return c, "", err
		}
	}
	return nil, "", fmt.Errorf("collection %q: no such file or workspace collection", ref)
}

func loadEnvironment(ws *workspace.Workspace, ref string) (*vars.Environment, error) {
	if isFile(ref) {
		data, err := os.ReadFile(ref)
		if err != nil {
			return nil, err
		}
		return vars.ParseEnvironment(data)
	}
	metas, err := ws.Environments()
	if err != nil {
		return nil, err
	}
	for _, m := range metas {
		if m.ID == ref || strings.EqualFold(m.Name, ref) {
			return ws.Environment(m.ID)
		}
	}
	return nil, fmt.Errorf("environment %q: no such file or workspace environment", ref)
}

func setVars(scope *vars.Scope, pairs []string) error {
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return fmt.Errorf("invalid variable %q, expected key=value", p)
		}
		scope.Set(k, v)
	}
	return nil
}

func runCmd(args []string, stdout, stderr io.Writer) (int, error) {
	o, err := parseArgs(args)
	if err != nil {
		return 1, err
	}
	if len(o.positional) != 1 {
		return 1, errors.New("usage: httpman run <collection> [options]")
	}
	ws, err := openWorkspace(o)
	if err != nil {
		return 1, err
	}
	c, baseDir, err := loadCollection(ws, o.positional[0])
	if err != nil {
		return 1, err
	}

	var env *vars.Scope
	envName := ""
	if ref := o.get("environment"); ref != "" {
		e, err := loadEnvironment(ws, ref)
		if err != nil {
			return 1, err
		}
		env = e.ToScope(e.Name)
		envName = e.Name
	}
	if len(o.values["env-var"]) > 0 {
		if env == nil {
			env = vars.NewScope("", nil)
		}
		if err := setVars(env, o.values["env-var"]); err != nil {
			return 1, err
		}
	}
	var globals *vars.Scope
	if ref := o.get("globals"); ref != "" {
		data, err := os.ReadFile(ref)
		if err != nil {
			return 1, err
		}
		g, err := vars.ParseEnvironment(data)
		if err != nil {
			return 1, err
		}
		globals = g.ToScope("globals")
	} else {
		globals = vars.NewScope("globals", nil)
	}
	if err := setVars(globals, o.values["global-var"]); err != nil {
		return 1, err
	}

	var data *runner.DataRows
	if p := o.get("iteration-data"); p != "" {
		if data, err = runner.LoadDataFile(p); err != nil {
			return 1, err
		}
	}
	iterations, err := o.intVal("iteration-count")
	if err != nil {
		return 1, err
	}
	delay, err := o.intVal("delay-request")
	if err != nil {
		return 1, err
	}
	timeout, err := o.intVal("timeout-request")
	if err != nil {
		return 1, err
	}
	scriptTimeout, err := o.intVal("timeout-script")
	if err != nil {
		return 1, err
	}
	if scriptTimeout > 0 {
		script.DefaultTimeout = time.Duration(scriptTimeout) * time.Millisecond
	}

	hopts := httpclient.DefaultOptions()
	hopts.Timeout = time.Duration(timeout) * time.Millisecond
	hopts.InsecureSkipVerify = o.flags["insecure"]
	hopts.FollowRedirects = !o.flags["ignore-redirects"]
	hopts.BaseDir = o.get("working-dir")
	if hopts.BaseDir == "" {
		hopts.BaseDir = baseDir
		if hopts.BaseDir == "" {
			hopts.BaseDir, _ = os.Getwd()
		}
	}
	jar := cookies.New()
	hopts.Jar = jar
	client := httpclient.New(hopts)
	defer client.Close()

	sess := runner.NewSession(client, jar, c, globals, env)

	reporters := strings.Split(o.get("reporters"), ",")
	if o.get("reporters") == "" {
		reporters = []string{"cli"}
	}
	color := !o.flags["no-color"] && o.get("color") != "off" && os.Getenv("NO_COLOR") == "" && (o.get("color") == "on" || isTerminal(stdout))
	var cliRep *cliReporter
	for _, r := range reporters {
		switch strings.TrimSpace(r) {
		case "cli":
			if !o.flags["silent"] {
				cliRep = newCLIReporter(stdout, color, o.flags["verbose"], !o.flags["disable-unicode"])
			}
		case "json", "junit":
		default:
			return 1, fmt.Errorf("unknown reporter %q", r)
		}
	}
	if cliRep != nil {
		sess.Console = cliRep.console
		cliRep.header(c.Info.Name)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	sum, err := sess.Run(ctx, runner.RunOptions{
		FolderID:   o.get("folder"),
		Iterations: iterations,
		Data:       data,
		Delay:      time.Duration(delay) * time.Millisecond,
		Bail:       o.flags["bail"],
	}, func(ev runner.RunEvent) {
		if cliRep != nil {
			cliRep.event(ev)
		}
	})
	if err != nil {
		return 1, err
	}
	sum.Environment = envName
	if cliRep != nil {
		cliRep.summary(sum)
	}

	for _, r := range reporters {
		switch strings.TrimSpace(r) {
		case "json":
			out, _ := json.MarshalIndent(jsonReport(sum), "", "  ")
			if p := o.get("reporter-json-export"); p != "" {
				if err := os.WriteFile(p, out, 0o644); err != nil {
					return 1, err
				}
			} else if cliRep == nil {
				fmt.Fprintln(stdout, string(out))
			} else if err := os.WriteFile("httpman-report.json", out, 0o644); err != nil {
				return 1, err
			}
		case "junit":
			p := o.get("reporter-junit-export")
			if p == "" {
				p = "httpman-report.xml"
			}
			if err := os.WriteFile(p, junitReport(sum), 0o644); err != nil {
				return 1, err
			}
		}
	}

	if p := o.get("export-environment"); p != "" {
		e := &vars.Environment{Name: envName, Values: []vars.Var{}, Scope: "environment"}
		if env != nil {
			e.Values = env.Vars
		}
		if err := writeJSON(p, e); err != nil {
			return 1, err
		}
	}
	if p := o.get("export-globals"); p != "" {
		if err := writeJSON(p, &vars.Environment{Name: "globals", Values: globals.Vars, Scope: "globals"}); err != nil {
			return 1, err
		}
	}
	if p := o.get("export-collection"); p != "" {
		c.Variable = nil
		for _, v := range sess.CollectionVars.Vars {
			c.Variable = append(c.Variable, collection.Variable{Key: v.Key, Value: v.Value, Type: v.Type, Disabled: !v.Enabled})
		}
		out, err := collection.Marshal(c)
		if err != nil {
			return 1, err
		}
		if err := os.WriteFile(p, out, 0o644); err != nil {
			return 1, err
		}
	}
	if sum.Failed() && !o.flags["suppress-exit-code"] {
		return 1, nil
	}
	return 0, nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "\t")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func importCmd(args []string, stdout io.Writer) error {
	o, err := parseArgs(args)
	if err != nil {
		return err
	}
	if len(o.positional) == 0 {
		return errors.New("usage: httpman import <file>...")
	}
	ws, err := openWorkspace(o)
	if err != nil {
		return err
	}
	for _, p := range o.positional {
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if c, err := ws.ImportCollection(data); err == nil {
			fmt.Fprintf(stdout, "imported collection %q (%s)\n", c.Info.Name, c.Info.PostmanID)
			continue
		}
		if e, err := ws.ImportEnvironment(data); err == nil {
			fmt.Fprintf(stdout, "imported environment %q (%s)\n", e.Name, e.ID)
			continue
		}
		return fmt.Errorf("%s: not a Postman collection or environment", p)
	}
	return nil
}

func listCmd(args []string, stdout io.Writer) error {
	o, err := parseArgs(args)
	if err != nil {
		return err
	}
	ws, err := openWorkspace(o)
	if err != nil {
		return err
	}
	cs, err := ws.Collections()
	if err != nil {
		return err
	}
	es, err := ws.Environments()
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Workspace: %s\n\nCollections:\n", ws.Dir)
	for _, c := range cs {
		fmt.Fprintf(stdout, "  %-38s %s\n", c.ID, c.Name)
	}
	fmt.Fprintln(stdout, "\nEnvironments:")
	for _, e := range es {
		fmt.Fprintf(stdout, "  %-38s %s\n", e.ID, e.Name)
	}
	return nil
}
