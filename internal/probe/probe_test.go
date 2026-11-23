package probe

import (
	"context"
	"testing"
	"time"

	"github.com/piolpro/rpcgauge/internal/testnode"
)

func opts() Options {
	return Options{
		Timeout:      5 * time.Second,
		Samples:      3,
		LogRanges:    []uint64{100, 1000, 10000},
		Methods:      DefaultMethods,
		Burst:        10,
		ArchiveBlock: 1,
	}
}

func TestRunFullNode(t *testing.T) {
	n := testnode.New(testnode.Config{
		ChainID: 1, Head: 20_000_000, Archive: true,
		Methods: []string{"debug_traceTransaction", "debug_traceBlockByNumber", "trace_block", "trace_transaction", "eth_getBlockReceipts"},
	})
	defer n.Close()

	r := Run(context.Background(), "full", n.URL, opts())
	if !r.Reachable || r.Error != "" {
		t.Fatalf("unexpected failure: %+v", r)
	}
	if r.ChainID != 1 || r.Head != 20_000_000 || r.ClientVersion != "FakeNode/v1.0.0" {
		t.Errorf("identity: chain %d head %d client %q", r.ChainID, r.Head, r.ClientVersion)
	}
	if r.Latency.Samples != 3 || r.Latency.Errors != 0 || r.Latency.P50Ms <= 0 {
		t.Errorf("latency: %+v", r.Latency)
	}
	if r.Archive != Supported {
		t.Errorf("archive = %s (%s)", r.Archive, r.ArchiveDetail)
	}
	if r.LogsMaxRange != 10000 {
		t.Errorf("logs max range = %d", r.LogsMaxRange)
	}
	for _, m := range r.Methods {
		if m.Support != Supported {
			t.Errorf("%s = %s (%s)", m.Method, m.Support, m.Detail)
		}
	}
	if r.Batch != Supported {
		t.Errorf("batch = %s", r.Batch)
	}
	if r.Burst == nil || r.Burst.OK != 10 {
		t.Errorf("burst = %+v", r.Burst)
	}
}

func TestRunRestrictedNode(t *testing.T) {
	n := testnode.New(testnode.Config{
		ChainID: 8453, Head: 30_000_000,
		LogsLimit: 2000, NoBatch: true, MaxConcurrent: 3, Delay: 20 * time.Millisecond,
	})
	defer n.Close()

	r := Run(context.Background(), "restricted", n.URL, opts())
	if !r.Reachable {
		t.Fatalf("unreachable: %s", r.Error)
	}
	if r.Archive != Unsupported {
		t.Errorf("archive = %s", r.Archive)
	}
	// 10000 is refused with "max block range 2000", so the hint is tried.
	if r.LogsMaxRange != 2000 {
		t.Errorf("logs max range = %d, ranges %+v", r.LogsMaxRange, r.LogsRanges)
	}
	if got := r.LogsRanges[len(r.LogsRanges)-1]; got.Blocks != 2000 || !got.OK {
		t.Errorf("last range = %+v", got)
	}
	for _, m := range r.Methods {
		if m.Support != Unsupported {
			t.Errorf("%s = %s (%s)", m.Method, m.Support, m.Detail)
		}
	}
	if r.Batch != Unsupported {
		t.Errorf("batch = %s", r.Batch)
	}
	if r.Burst == nil || r.Burst.RateLimited == 0 || r.Burst.OK+r.Burst.RateLimited != r.Burst.Requests {
		t.Errorf("burst = %+v", r.Burst)
	}
}

func TestRunUnreachable(t *testing.T) {
	n := testnode.New(testnode.Config{Down: true})
	defer n.Close()
	r := Run(context.Background(), "down", n.URL, opts())
	if r.Reachable || r.Error == "" {
		t.Fatalf("expected unreachable, got %+v", r)
	}
	if n.Calls["eth_blockNumber"] != 0 {
		t.Errorf("kept probing an unreachable node")
	}
}

func TestPercentile(t *testing.T) {
	v := []float64{10, 20, 30, 40, 50}
	if p := percentile(v, 50); p != 30 {
		t.Errorf("p50 = %v", p)
	}
	if p := percentile(v, 100); p != 50 {
		t.Errorf("p100 = %v", p)
	}
	if p := percentile([]float64{7}, 95); p != 7 {
		t.Errorf("single = %v", p)
	}
}
