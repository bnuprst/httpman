package runner

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bnuprst/httpman/internal/collection"
	"github.com/bnuprst/httpman/internal/vars"
)

// DataRows is iteration data loaded from a CSV or JSON file.
type DataRows struct {
	Columns []string         `json:"columns"`
	Rows    []map[string]any `json:"rows"`
}

// LoadDataFile reads a Postman runner data file (CSV with header or JSON array).
func LoadDataFile(path string) (*DataRows, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseData(b, strings.EqualFold(filepath.Ext(path), ".json"))
}

// ParseData parses iteration data. When isJSON is false, JSON is still
// detected by content.
func ParseData(b []byte, isJSON bool) (*DataRows, error) {
	b = bytes.TrimPrefix(bytes.TrimSpace(b), []byte("\xef\xbb\xbf"))
	if isJSON || (len(b) > 0 && (b[0] == '[' || b[0] == '{')) {
		var rows []map[string]any
		if err := json.Unmarshal(b, &rows); err != nil {
			var one map[string]any
			if json.Unmarshal(b, &one) != nil {
				return nil, fmt.Errorf("data file must be a JSON array of objects: %w", err)
			}
			rows = []map[string]any{one}
		}
		d := &DataRows{Rows: rows}
		seen := map[string]bool{}
		for _, r := range rows {
			for k := range r {
				if !seen[k] {
					seen[k] = true
					d.Columns = append(d.Columns, k)
				}
			}
		}
		return d, nil
	}
	r := csv.NewReader(bytes.NewReader(b))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("invalid CSV: %w", err)
	}
	if len(records) == 0 {
		return &DataRows{}, nil
	}
	d := &DataRows{Columns: records[0]}
	for _, rec := range records[1:] {
		if len(rec) == 1 && rec[0] == "" {
			continue
		}
		row := map[string]any{}
		for i, col := range d.Columns {
			if i < len(rec) {
				row[col] = rec[i]
			} else {
				row[col] = ""
			}
		}
		d.Rows = append(d.Rows, row)
	}
	return d, nil
}

// RunOptions configure a collection run.
type RunOptions struct {
	FolderID   string // run only this folder (or single request)
	Iterations int
	Data       *DataRows
	Delay      time.Duration
	Bail       bool     // stop on first failure
	Only       []string // optional explicit ordered list of request ids to run
}

// RunEvent is emitted during a run.
type RunEvent struct {
	Type      string     `json:"type"` // start, iteration, execution, done
	Iteration int        `json:"iteration"`
	Execution *Execution `json:"execution,omitempty"`
	Summary   *Summary   `json:"summary,omitempty"`
	Total     int        `json:"total,omitempty"`
}

// Summary aggregates a run.
type Summary struct {
	Collection   string        `json:"collection"`
	Environment  string        `json:"environment,omitempty"`
	Iterations   int           `json:"iterations"`
	Requests     int           `json:"requests"`
	FailedReqs   int           `json:"failedRequests"`
	Tests        int           `json:"tests"`
	TestsPassed  int           `json:"testsPassed"`
	TestsFailed  int           `json:"testsFailed"`
	TestsSkipped int           `json:"testsSkipped"`
	ScriptErrors int           `json:"scriptErrors"`
	Duration     time.Duration `json:"duration"`
	AvgResponse  float64       `json:"avgResponseTime"`
	Started      time.Time     `json:"started"`
	Stopped      bool          `json:"stopped,omitempty"`
	Executions   []*Execution  `json:"executions"`
}

// Failed reports whether anything in the run failed.
func (s *Summary) Failed() bool { return s.FailedReqs > 0 || s.TestsFailed > 0 || s.ScriptErrors > 0 }

type entry struct {
	item    *collection.Item
	parents []*collection.Item
}

func (s *Session) plan(opts RunOptions) ([]entry, error) {
	if s.Collection == nil {
		return nil, fmt.Errorf("no collection")
	}
	var list []entry
	collect := func(items []*collection.Item, base []*collection.Item) {
		var walk func(items []*collection.Item, parents []*collection.Item)
		walk = func(items []*collection.Item, parents []*collection.Item) {
			for _, it := range items {
				if it.IsFolder() {
					walk(it.Item, append(append([]*collection.Item{}, parents...), it))
				} else if it.Request != nil {
					list = append(list, entry{it, parents})
				}
			}
		}
		walk(items, base)
	}
	if opts.FolderID != "" {
		it, parents := s.Collection.Find(opts.FolderID)
		if it == nil {
			return nil, fmt.Errorf("folder or request %q not found", opts.FolderID)
		}
		if it.IsFolder() {
			collect(it.Item, append(parents, it))
		} else {
			list = append(list, entry{it, parents})
		}
	} else {
		collect(s.Collection.Item, nil)
	}
	if len(opts.Only) > 0 {
		byID := map[string]entry{}
		for _, e := range list {
			byID[e.item.ID] = e
		}
		var filtered []entry
		for _, id := range opts.Only {
			if e, ok := byID[id]; ok {
				filtered = append(filtered, e)
			}
		}
		list = filtered
	}
	return list, nil
}

// Run executes a collection (or folder) for the configured iterations.
func (s *Session) Run(ctx context.Context, opts RunOptions, emit func(RunEvent)) (*Summary, error) {
	if emit == nil {
		emit = func(RunEvent) {}
	}
	list, err := s.plan(opts)
	if err != nil {
		return nil, err
	}
	iterations := opts.Iterations
	if opts.Data != nil && len(opts.Data.Rows) > 0 && iterations <= 0 {
		iterations = len(opts.Data.Rows)
	}
	if iterations <= 0 {
		iterations = 1
	}
	sum := &Summary{Collection: s.Collection.Info.Name, Iterations: iterations, Started: time.Now(), Executions: []*Execution{}}
	if s.Environment != nil {
		sum.Environment = s.Environment.Name
	}
	emit(RunEvent{Type: "start", Total: len(list) * iterations})
	var totalTime float64

	find := func(name string) int {
		for i, e := range list {
			if e.item.ID == name {
				return i
			}
		}
		for i, e := range list {
			if e.item.Name == name {
				return i
			}
		}
		return -1
	}

outer:
	for iter := 0; iter < iterations; iter++ {
		var data *vars.Scope
		if opts.Data != nil && len(opts.Data.Rows) > 0 {
			data = vars.FromMap("iterationData", opts.Data.Rows[iter%len(opts.Data.Rows)], opts.Data.Columns)
		}
		emit(RunEvent{Type: "iteration", Iteration: iter})
		for i := 0; i < len(list); {
			if ctx.Err() != nil {
				sum.Stopped = true
				break outer
			}
			e := list[i]
			ex := s.Execute(ctx, e.item, e.parents, IterationInfo{Iteration: iter, IterationCount: iterations, Data: data})
			sum.Executions = append(sum.Executions, ex)
			if !ex.Skipped {
				sum.Requests++
			}
			if ex.Error != "" {
				sum.FailedReqs++
			}
			if ex.Response != nil {
				totalTime += ex.Response.Time
			}
			sum.ScriptErrors += len(ex.ScriptErrors)
			for _, t := range ex.Tests {
				sum.Tests++
				switch {
				case t.Skipped:
					sum.TestsSkipped++
				case t.Passed:
					sum.TestsPassed++
				default:
					sum.TestsFailed++
				}
			}
			emit(RunEvent{Type: "execution", Iteration: iter, Execution: ex})
			if opts.Bail && ex.Failed() {
				sum.Stopped = true
				break outer
			}
			next := i + 1
			if ex.NextRequest != nil {
				if ex.NextRequest.Value == nil {
					break // stop this iteration
				}
				if j := find(*ex.NextRequest.Value); j >= 0 {
					next = j
				} else {
					s.log("warn", fmt.Sprintf("setNextRequest: request %q not found, stopping iteration", *ex.NextRequest.Value), nil)
					break
				}
			}
			i = next
			if opts.Delay > 0 && i < len(list) {
				select {
				case <-ctx.Done():
				case <-time.After(opts.Delay):
				}
			}
		}
	}
	sum.Duration = time.Since(sum.Started)
	if sum.Requests > 0 {
		sum.AvgResponse = totalTime / float64(sum.Requests)
	}
	emit(RunEvent{Type: "done", Summary: sum})
	return sum, nil
}
