package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/piolpro/rpcgauge/internal/probe"
)

func result(name string, chain, head uint64, p50 float64) *probe.Result {
	return &probe.Result{
		Name: name, Reachable: true, ChainID: chain, Head: head,
		Latency: probe.Latency{Samples: 5, P50Ms: p50, P95Ms: p50 + 5},
		Archive: probe.Unsupported, Batch: probe.Supported,
	}
}

func names(es []*Entry) string {
	var s []string
	for _, e := range es {
		s = append(s, e.Name)
	}
	return strings.Join(s, ",")
}

func TestEvaluateRanksByStatusLagLatency(t *testing.T) {
	slow := result("slow", 1, 100, 300)
	fast := result("fast", 1, 100, 50)
	behind := result("behind", 1, 98, 10)
	stale := result("stale", 1, 90, 10)
	down := &probe.Result{Name: "down", Error: "eth_chainId: http 502"}
	base := result("base", 8453, 500, 80)

	es := Evaluate([]*probe.Result{slow, down, stale, base, behind, fast}, nil, Criteria{MaxLag: 5})
	if got := names(es); got != "fast,slow,behind,stale,base,down" {
		t.Fatalf("order = %s", got)
	}
	byName := map[string]*Entry{}
	for _, e := range es {
		byName[e.Name] = e
	}
	if *byName["behind"].Lag != 2 || byName["behind"].Status != OK {
		t.Errorf("behind: lag %d status %s", *byName["behind"].Lag, byName["behind"].Status)
	}
	if byName["stale"].Status != Fail || *byName["stale"].Lag != 10 {
		t.Errorf("stale: %+v", byName["stale"])
	}
	if byName["base"].Rank != 1 || *byName["base"].Lag != 0 {
		t.Errorf("base should rank first on its own chain: %+v", byName["base"])
	}
	if byName["down"].Status != Fail || byName["down"].Rank != 0 {
		t.Errorf("down: %+v", byName["down"])
	}
	if Worst(es) != Fail {
		t.Errorf("worst = %s", Worst(es))
	}
}

func TestEvaluateUsesSyncedHeads(t *testing.T) {
	a := result("a", 1, 100, 10)
	b := result("b", 1, 90, 10)
	es := Evaluate([]*probe.Result{a, b}, map[int]uint64{0: 120, 1: 119}, Criteria{MaxLag: 5})
	for _, e := range es {
		if e.Status != OK {
			t.Errorf("%s: %v", e.Name, e.Problems)
		}
	}
	if es[1].Name != "b" || *es[1].Lag != 1 || es[1].Head != 119 {
		t.Errorf("b: head %d lag %d", es[1].Head, *es[1].Lag)
	}
}

func TestRequirementsAndWarnings(t *testing.T) {
	r := result("r", 1, 100, 10)
	r.LogsMaxRange = 1000
	r.Methods = []probe.MethodResult{
		{Method: "debug_traceTransaction", Support: probe.Supported},
		{Method: "trace_block", Support: probe.Unsupported},
	}
	r.Burst = &probe.Burst{Requests: 20, OK: 15, RateLimited: 5}

	reqs, err := ParseRequirements("debug, logs=1000")
	if err != nil {
		t.Fatal(err)
	}
	es := Evaluate([]*probe.Result{r}, nil, Criteria{MaxLag: 5, Require: reqs})
	if es[0].Status != Warn || len(es[0].Warnings) != 1 {
		t.Errorf("want warn for rate limiting, got %s %v %v", es[0].Status, es[0].Problems, es[0].Warnings)
	}

	reqs, _ = ParseRequirements("archive,trace,logs=10000,eth_getBlockReceipts")
	es = Evaluate([]*probe.Result{r}, nil, Criteria{MaxLag: 5, Require: reqs, ExpectChainID: 10})
	if es[0].Status != Fail || len(es[0].Problems) != 5 {
		t.Errorf("want 5 problems, got %v", es[0].Problems)
	}
}

func TestParseRequirementsErrors(t *testing.T) {
	for _, bad := range []string{"logs", "logs=abc", "logs=0", "archive=1"} {
		if _, err := ParseRequirements(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

func TestRenderers(t *testing.T) {
	r := result("node-a", 8453, 100, 42)
	r.LogsRanges = []probe.RangeResult{{Blocks: 1000, OK: true}, {Blocks: 10000, OK: true}}
	r.LogsMaxRange = 10000
	down := &probe.Result{Name: "node-b", Error: "eth_chainId: http 502"}
	es := Evaluate([]*probe.Result{r, down}, nil, Criteria{MaxLag: 5})

	var tbl bytes.Buffer
	if err := Table(&tbl, es); err != nil {
		t.Fatal(err)
	}
	out := tbl.String()
	for _, want := range []string{"ENDPOINT", "node-a", "base", "42ms", ">=10k", "node-b: FAIL unreachable"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}

	var md bytes.Buffer
	Markdown(&md, es)
	if !strings.HasPrefix(md.String(), "| # | ENDPOINT |") || !strings.Contains(md.String(), "- node-b: FAIL") {
		t.Errorf("markdown:\n%s", md.String())
	}

	var js bytes.Buffer
	if err := JSON(&js, "v0.0.0-test", es); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Status    string `json:"status"`
		Endpoints []struct {
			Name   string `json:"name"`
			Rank   int    `json:"rank"`
			Lag    *int64 `json:"lag"`
			Status string `json:"status"`
		} `json:"endpoints"`
	}
	if err := json.Unmarshal(js.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Status != "fail" || len(doc.Endpoints) != 2 || doc.Endpoints[0].Rank != 1 || doc.Endpoints[1].Lag != nil {
		t.Errorf("json: %s", js.String())
	}
}

func TestHumanBlocks(t *testing.T) {
	for in, want := range map[uint64]string{50: "50", 2000: "2k", 2500: "2500", 1_000_000: "1m"} {
		if got := humanBlocks(in); got != want {
			t.Errorf("humanBlocks(%d) = %s, want %s", in, got, want)
		}
	}
}
