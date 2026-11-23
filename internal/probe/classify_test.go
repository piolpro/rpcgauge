package probe

import (
	"errors"
	"testing"

	"github.com/piolpro/rpcgauge/internal/rpc"
)

func TestClassifyMethodError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want Support
	}{
		{"success", nil, Supported},
		{"method not found code", &rpc.Error{Code: -32601, Message: "whatever"}, Unsupported},
		{"geth not available", &rpc.Error{Code: -32000, Message: "the method debug_traceTransaction does not exist/is not available"}, Unsupported},
		{"not allowed", &rpc.Error{Code: -32600, Message: "Method trace_block not allowed"}, Unsupported},
		{"request filter", &rpc.Error{Code: -32611, Message: "rejected due to request filter settings"}, Unsupported},
		{"paid plan", &rpc.Error{Code: 35, Message: "this method is available on paid plan, please upgrade to paid plan"}, Unsupported},
		{"invalid params", &rpc.Error{Code: -32602, Message: "invalid argument 0: hex string has length 2"}, Supported},
		{"tx not found", &rpc.Error{Code: -32000, Message: "transaction 0x00 not found"}, Supported},
		{"genesis", &rpc.Error{Code: -32000, Message: "genesis is not traceable"}, Supported},
		{"http 401", &rpc.HTTPError{Status: 401, Body: "Only core evm requests are allowed."}, Unsupported},
		{"http 406", &rpc.HTTPError{Status: 406, Body: "406 Not Acceptable"}, Unsupported},
		{"http 429", &rpc.HTTPError{Status: 429, Body: "Too Many Requests"}, Unknown},
		{"rate limit message", &rpc.Error{Code: -32005, Message: "Rate limit exceeded"}, Unknown},
		{"transport", errors.New("dial tcp: i/o timeout"), Unknown},
	}
	for _, c := range cases {
		if got := ClassifyMethodError(c.err); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

func TestIsRateLimited(t *testing.T) {
	yes := []error{
		&rpc.HTTPError{Status: 429},
		&rpc.Error{Code: 15, Message: "You reached Public endpoint rate limit, please upgrade to paid plan"},
		&rpc.Error{Code: -32011, Message: "request limit reached"},
		&rpc.Error{Code: -32005, Message: "project ID request rate exceeded"},
	}
	for _, e := range yes {
		if !IsRateLimited(e) {
			t.Errorf("expected rate limited: %v", e)
		}
	}
	no := []error{
		nil,
		&rpc.Error{Code: -32005, Message: "query exceeds max block range 10000"},
		&rpc.Error{Code: -32602, Message: "exceeded the maximum block range"},
		&rpc.Error{Code: -32005, Message: "query returned more than 10000 results"},
	}
	for _, e := range no {
		if IsRateLimited(e) {
			t.Errorf("expected not rate limited: %v", e)
		}
	}
}

func TestRangeHint(t *testing.T) {
	cases := []struct {
		msg        string
		lo, refuse uint64
		want       uint64
	}{
		{"ranges over 100 blocks are not supported on free plan", 0, 1000, 100},
		{"eth_getLogs is limited to 0 - 50 blocks range", 0, 100, 50},
		{"range 49999 exceeds limit of 10000", 5000, 50000, 10000},
		{"range 49999 exceeds limit of 10000", 10000, 50000, 0},
		{"range 9999 exceeds limit of 2,000", 1000, 10000, 2000},
		{"You can make eth_getLogs requests with up to a 10 block range. Based on your parameters, this block range should work: [0x194089c, 0x19408a5]", 0, 100, 10},
		{"exceed maximum block range: 50000", 50000, 100000, 0},
		{"no numbers here", 0, 1000, 0},
	}
	for _, c := range cases {
		if got := rangeHint(c.msg, c.lo, c.refuse); got != c.want {
			t.Errorf("%q: got %d, want %d", c.msg, got, c.want)
		}
	}
}

func TestParseQuantity(t *testing.T) {
	n, err := ParseQuantity("0x2105")
	if err != nil || n.Uint64() != 8453 {
		t.Fatalf("got %v %v", n, err)
	}
	for _, bad := range []string{"", "0x", "2105", "0xzz"} {
		if _, err := ParseQuantity(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
	if Quantity(255) != "0xff" {
		t.Errorf("Quantity(255) = %s", Quantity(255))
	}
}
