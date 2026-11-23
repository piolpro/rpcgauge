// Package report ranks probe results and renders them as a table, JSON or
// Markdown.
package report

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/piolpro/rpcgauge/internal/probe"
)

// Status is the verdict for one endpoint.
type Status string

const (
	OK   Status = "ok"
	Warn Status = "warn"
	Fail Status = "fail"
)

func (s Status) weight() int {
	switch s {
	case OK:
		return 0
	case Warn:
		return 1
	}
	return 2
}

// Requirement is a feature an endpoint must have to pass.
type Requirement struct {
	Name      string // archive, batch, logs, or a method name / "debug" / "trace"
	MinBlocks uint64 // for logs
}

// ParseRequirements parses "archive,trace,logs=10000,eth_getBlockReceipts".
func ParseRequirements(s string) ([]Requirement, error) {
	var out []Requirement
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, val, hasVal := strings.Cut(part, "=")
		r := Requirement{Name: name}
		if name == "logs" {
			if !hasVal {
				return nil, fmt.Errorf("requirement %q: use logs=<blocks>", part)
			}
			n, err := strconv.ParseUint(val, 10, 64)
			if err != nil || n == 0 {
				return nil, fmt.Errorf("requirement %q: invalid block count", part)
			}
			r.MinBlocks = n
		} else if hasVal {
			return nil, fmt.Errorf("requirement %q: only logs takes a value", part)
		}
		out = append(out, r)
	}
	return out, nil
}

// Criteria decide when an endpoint warns or fails.
type Criteria struct {
	ExpectChainID uint64  // 0 = any
	MaxLag        int64   // fail when lag is above this; <0 disables
	MaxLatencyMs  float64 // fail when p50 is above this; 0 disables
	Require       []Requirement
}

// Entry is a ranked, evaluated endpoint.
type Entry struct {
	*probe.Result
	Rank     int      `json:"rank"`
	Lag      *int64   `json:"lag"`
	Status   Status   `json:"status"`
	Problems []string `json:"problems,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// Debug reports whether any debug_* method is supported.
func (e *Entry) Debug() probe.Support { return familySupport(e.Methods, "debug_") }

// Trace reports whether any trace_* method is supported.
func (e *Entry) Trace() probe.Support { return familySupport(e.Methods, "trace_") }

func familySupport(ms []probe.MethodResult, prefix string) probe.Support {
	seen := false
	unknown := false
	for _, m := range ms {
		if !strings.HasPrefix(m.Method, prefix) {
			continue
		}
		seen = true
		switch m.Support {
		case probe.Supported:
			return probe.Supported
		case probe.Unknown:
			unknown = true
		}
	}
	if !seen || unknown {
		return probe.Unknown
	}
	return probe.Unsupported
}

func methodSupport(ms []probe.MethodResult, name string) (probe.Support, bool) {
	for _, m := range ms {
		if m.Method == name {
			return m.Support, true
		}
	}
	return probe.Unknown, false
}

// Evaluate computes lag per chain (heads maps endpoint index to a head
// sampled at the same moment for all endpoints; missing entries fall back to
// the head seen during probing), applies criteria, and ranks endpoints within
// each chain: ok before warn before fail, then lowest lag, then lowest p50.
func Evaluate(results []*probe.Result, heads map[int]uint64, c Criteria) []*Entry {
	entries := make([]*Entry, len(results))
	maxHead := map[uint64]uint64{}
	headOf := func(i int) uint64 {
		if h, ok := heads[i]; ok && h > 0 {
			return h
		}
		return results[i].Head
	}
	for i, r := range results {
		entries[i] = &Entry{Result: r}
		if r.Reachable {
			if h := headOf(i); h > maxHead[r.ChainID] {
				maxHead[r.ChainID] = h
			}
		}
	}
	for i, e := range entries {
		if e.Reachable {
			if h := headOf(i); h > 0 {
				e.Head = h
				lag := int64(maxHead[e.ChainID] - h)
				e.Lag = &lag
			}
		}
		judge(e, c)
	}

	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Reachable != b.Reachable {
			return a.Reachable
		}
		if a.ChainID != b.ChainID {
			return a.ChainID < b.ChainID
		}
		if a.Status.weight() != b.Status.weight() {
			return a.Status.weight() < b.Status.weight()
		}
		la, lb := lagOr(a), lagOr(b)
		if la != lb {
			return la < lb
		}
		return a.Latency.P50Ms < b.Latency.P50Ms
	})
	rank := map[uint64]int{}
	for _, e := range entries {
		if !e.Reachable {
			continue
		}
		rank[e.ChainID]++
		e.Rank = rank[e.ChainID]
	}
	return entries
}

func lagOr(e *Entry) int64 {
	if e.Lag == nil {
		return 1 << 62
	}
	return *e.Lag
}

func judge(e *Entry, c Criteria) {
	if !e.Reachable {
		e.Status = Fail
		e.Problems = append(e.Problems, "unreachable: "+e.Error)
		return
	}
	if e.Error != "" {
		e.Problems = append(e.Problems, e.Error)
	}
	if c.ExpectChainID != 0 && e.ChainID != c.ExpectChainID {
		e.Problems = append(e.Problems, fmt.Sprintf("chain id %d, expected %d", e.ChainID, c.ExpectChainID))
	}
	if c.MaxLag >= 0 && e.Lag != nil && *e.Lag > c.MaxLag {
		e.Problems = append(e.Problems, fmt.Sprintf("%d blocks behind (max %d)", *e.Lag, c.MaxLag))
	}
	if c.MaxLatencyMs > 0 && e.Latency.P50Ms > c.MaxLatencyMs {
		e.Problems = append(e.Problems, fmt.Sprintf("p50 latency %.0fms (max %.0fms)", e.Latency.P50Ms, c.MaxLatencyMs))
	}
	for _, r := range c.Require {
		if msg := missing(e, r); msg != "" {
			e.Problems = append(e.Problems, msg)
		}
	}
	if e.Latency.Errors > 0 {
		e.Warnings = append(e.Warnings, fmt.Sprintf("%d of %d latency samples failed", e.Latency.Errors, e.Latency.Samples))
	}
	if e.Burst != nil && e.Burst.RateLimited > 0 {
		e.Warnings = append(e.Warnings, fmt.Sprintf("rate limited on %d of %d burst requests", e.Burst.RateLimited, e.Burst.Requests))
	}
	if e.Burst != nil && e.Burst.Errors > 0 {
		e.Warnings = append(e.Warnings, fmt.Sprintf("%d of %d burst requests failed", e.Burst.Errors, e.Burst.Requests))
	}
	switch {
	case len(e.Problems) > 0:
		e.Status = Fail
	case len(e.Warnings) > 0:
		e.Status = Warn
	default:
		e.Status = OK
	}
}

func missing(e *Entry, r Requirement) string {
	switch r.Name {
	case "archive":
		if e.Archive != probe.Supported {
			return "archive state not available"
		}
	case "batch":
		if e.Batch != probe.Supported {
			return "batch requests not supported"
		}
	case "logs":
		if e.LogsMaxRange < r.MinBlocks {
			return fmt.Sprintf("eth_getLogs range %d < required %d", e.LogsMaxRange, r.MinBlocks)
		}
	case "debug":
		if e.Debug() != probe.Supported {
			return "no debug_* methods"
		}
	case "trace":
		if e.Trace() != probe.Supported {
			return "no trace_* methods"
		}
	default:
		s, probed := methodSupport(e.Methods, r.Name)
		if !probed {
			return r.Name + " was not probed (add it to --methods)"
		}
		if s != probe.Supported {
			return r.Name + " not supported"
		}
	}
	return ""
}

// Worst returns the most severe status among entries.
func Worst(entries []*Entry) Status {
	w := OK
	for _, e := range entries {
		if e.Status.weight() > w.weight() {
			w = e.Status
		}
	}
	return w
}
