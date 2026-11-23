// Package probe runs the health checks against a single EVM JSON-RPC endpoint.
package probe

import (
	"context"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/piolpro/rpcgauge/internal/rpc"
)

// Options controls which checks run and how hard they push.
type Options struct {
	Timeout      time.Duration
	Samples      int
	LogRanges    []uint64
	Methods      []string
	Burst        int
	ArchiveBlock uint64
	Headers      map[string]string
}

// DefaultMethods are probed when no method list is given.
var DefaultMethods = []string{
	"debug_traceTransaction",
	"debug_traceBlockByNumber",
	"trace_block",
	"trace_transaction",
	"eth_getBlockReceipts",
}

// Latency summarises eth_blockNumber round trips in milliseconds.
type Latency struct {
	Samples int     `json:"samples"`
	Errors  int     `json:"errors"`
	MinMs   float64 `json:"min_ms"`
	P50Ms   float64 `json:"p50_ms"`
	P95Ms   float64 `json:"p95_ms"`
	MaxMs   float64 `json:"max_ms"`
}

// RangeResult is one eth_getLogs block-range attempt.
type RangeResult struct {
	Blocks      uint64  `json:"blocks"`
	OK          bool    `json:"ok"`
	Ms          float64 `json:"ms"`
	RateLimited bool    `json:"rate_limited,omitempty"`
	Error       string  `json:"error,omitempty"`
}

// MethodResult is one method-support probe.
type MethodResult struct {
	Method  string  `json:"method"`
	Support Support `json:"support"`
	Detail  string  `json:"detail,omitempty"`
}

// Burst is the outcome of firing concurrent requests at once.
type Burst struct {
	Requests    int     `json:"requests"`
	OK          int     `json:"ok"`
	RateLimited int     `json:"rate_limited"`
	Errors      int     `json:"errors"`
	DurationMs  float64 `json:"duration_ms"`
}

// Result is everything learned about one endpoint.
type Result struct {
	Name          string         `json:"name"`
	URL           string         `json:"url"`
	Reachable     bool           `json:"reachable"`
	Error         string         `json:"error,omitempty"`
	ChainID       uint64         `json:"chain_id,omitempty"`
	ClientVersion string         `json:"client_version,omitempty"`
	Head          uint64         `json:"head,omitempty"`
	Latency       Latency        `json:"latency"`
	Archive       Support        `json:"archive"`
	ArchiveDetail string         `json:"archive_detail,omitempty"`
	LogsMaxRange  uint64         `json:"logs_max_range"`
	LogsRanges    []RangeResult  `json:"logs_ranges,omitempty"`
	Methods       []MethodResult `json:"methods,omitempty"`
	Batch         Support        `json:"batch"`
	Burst         *Burst         `json:"burst,omitempty"`

	client *rpc.Client
}

// Client returns the RPC client used for this endpoint.
func (r *Result) Client() *rpc.Client { return r.client }

// Run probes one endpoint. It never returns an error: failures are recorded
// on the result so they can be reported and ranked.
func Run(ctx context.Context, name, url string, opt Options) *Result {
	c := rpc.New(url, opt.Headers, opt.Timeout)
	res := &Result{Name: name, client: c, Archive: Unknown, Batch: Unknown}

	var chainHex string
	if _, err := c.Call(ctx, "eth_chainId", nil, &chainHex); err != nil {
		res.Error = "eth_chainId: " + err.Error()
		return res
	}
	id, err := ParseQuantity(chainHex)
	if err != nil {
		res.Error = "eth_chainId: " + err.Error()
		return res
	}
	res.Reachable = true
	res.ChainID = id.Uint64()

	var cv string
	if _, err := c.Call(ctx, "web3_clientVersion", nil, &cv); err == nil {
		res.ClientVersion = cv
	}

	var latErr error
	res.Latency, res.Head, latErr = measureLatency(ctx, c, opt.Samples)
	if res.Head == 0 {
		res.Error = "eth_blockNumber failed on every sample"
		if latErr != nil {
			res.Error += ": " + truncate(latErr.Error(), 160)
		}
		return res
	}

	res.Archive, res.ArchiveDetail = checkArchive(ctx, c, opt.ArchiveBlock)
	res.LogsRanges, res.LogsMaxRange = checkLogs(ctx, c, res.Head, opt.LogRanges)
	res.Methods = checkMethods(ctx, c, opt.Methods)
	res.Batch = checkBatch(ctx, c)
	if opt.Burst > 0 {
		res.Burst = runBurst(ctx, c, opt.Burst)
	}
	return res
}

// Head fetches the current block number.
func Head(ctx context.Context, c *rpc.Client) (uint64, error) {
	var h string
	if _, err := c.Call(ctx, "eth_blockNumber", nil, &h); err != nil {
		return 0, err
	}
	n, err := ParseQuantity(h)
	if err != nil {
		return 0, err
	}
	return n.Uint64(), nil
}

func measureLatency(ctx context.Context, c *rpc.Client, samples int) (Latency, uint64, error) {
	if samples < 1 {
		samples = 1
	}
	var durs []float64
	var head uint64
	var lastErr error
	lat := Latency{Samples: samples}
	for i := 0; i < samples; i++ {
		var h string
		d, err := c.Call(ctx, "eth_blockNumber", nil, &h)
		if err != nil {
			lat.Errors++
			lastErr = err
			continue
		}
		n, err := ParseQuantity(h)
		if err != nil {
			lat.Errors++
			lastErr = err
			continue
		}
		head = n.Uint64()
		durs = append(durs, ms(d))
	}
	if len(durs) > 0 {
		sort.Float64s(durs)
		lat.MinMs = durs[0]
		lat.MaxMs = durs[len(durs)-1]
		lat.P50Ms = percentile(durs, 50)
		lat.P95Ms = percentile(durs, 95)
	}
	return lat, head, lastErr
}

func checkArchive(ctx context.Context, c *rpc.Client, block uint64) (Support, string) {
	var bal string
	_, err := call(ctx, c, "eth_getBalance", []any{zeroAddress, Quantity(block)}, &bal)
	if err == nil {
		return Supported, ""
	}
	if IsRateLimited(err) {
		return Unknown, err.Error()
	}
	return Unsupported, truncate(err.Error(), 200)
}

// checkLogs tries eth_getLogs over growing block ranges ending a few blocks
// behind head. The filter matches the zero address, which never emits logs,
// so responses stay empty and only the provider's range limit is measured.
// When a range is refused and the error names a smaller limit ("max range
// 2000"), that limit is tried too, so the result is exact for most providers.
func checkLogs(ctx context.Context, c *rpc.Client, head uint64, ranges []uint64) ([]RangeResult, uint64) {
	const safety = 5
	var out []RangeResult
	var maxOK uint64
	if head <= safety {
		return nil, 0
	}
	to := head - safety
	try := func(n uint64) (RangeResult, error) {
		from := uint64(0)
		if to+1 > n {
			from = to + 1 - n
		}
		filter := map[string]any{
			"fromBlock": Quantity(from),
			"toBlock":   Quantity(to),
			"address":   zeroAddress,
		}
		var logs []any
		d, err := call(ctx, c, "eth_getLogs", []any{filter}, &logs)
		rr := RangeResult{Blocks: n, Ms: ms(d), OK: err == nil}
		if err != nil {
			rr.Error = truncate(err.Error(), 200)
			rr.RateLimited = IsRateLimited(err)
		}
		return rr, err
	}
	for _, n := range ranges {
		if n == 0 || n <= maxOK {
			continue
		}
		rr, err := try(n)
		out = append(out, rr)
		if err == nil {
			maxOK = n
			continue
		}
		if !rr.RateLimited {
			if hint := rangeHint(err.Error(), maxOK, n); hint > 0 {
				if hr, herr := try(hint); herr == nil {
					maxOK = hint
					out = append(out, hr)
				} else {
					out = append(out, hr)
				}
			}
		}
		break
	}
	return out, maxOK
}

var numberRe = regexp.MustCompile(`\b\d[\d,]*\b`)

// rangeHint picks the largest number in msg above lo and below the refused
// range (minus one, since some providers echo the span as to-from), which is
// usually the provider's advertised block-range limit.
func rangeHint(msg string, lo, refused uint64) uint64 {
	hi := refused - 1
	var best uint64
	for _, m := range numberRe.FindAllString(msg, -1) {
		n, err := strconv.ParseUint(strings.ReplaceAll(m, ",", ""), 10, 64)
		if err != nil {
			continue
		}
		if n > lo && n < hi && n > best {
			best = n
		}
	}
	return best
}

// call retries a request that was rate limited, backing off briefly, so the
// feature checks are not skewed by the checker's own request rate.
func call(ctx context.Context, c *rpc.Client, method string, params []any, out any) (time.Duration, error) {
	var d time.Duration
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return d, err
			case <-time.After(time.Duration(attempt) * 750 * time.Millisecond):
			}
		}
		d, err = c.Call(ctx, method, params, out)
		if !IsRateLimited(err) {
			return d, err
		}
	}
	return d, err
}

// methodParams returns cheap probe parameters: a zero transaction hash or a
// block far in the future. A node that serves the method answers "not found"
// or null without doing work or needing historical state.
func methodParams(method string) []any {
	switch method {
	case "debug_traceTransaction", "trace_transaction", "trace_replayTransaction", "eth_getTransactionReceipt", "eth_getTransactionByHash":
		return []any{zeroHash}
	case "debug_traceBlockByNumber":
		return []any{futureBlock, map[string]any{"tracer": "callTracer"}}
	case "trace_block", "eth_getBlockReceipts":
		return []any{futureBlock}
	case "trace_replayBlockTransactions":
		return []any{futureBlock, []string{"trace"}}
	}
	return nil
}

func checkMethods(ctx context.Context, c *rpc.Client, methods []string) []MethodResult {
	out := make([]MethodResult, 0, len(methods))
	for _, m := range methods {
		_, err := call(ctx, c, m, methodParams(m), nil)
		mr := MethodResult{Method: m, Support: ClassifyMethodError(err)}
		if err != nil {
			mr.Detail = truncate(err.Error(), 160)
		}
		out = append(out, mr)
	}
	return out
}

func checkBatch(ctx context.Context, c *rpc.Client) Support {
	resps, _, err := c.Batch(ctx, []rpc.Call{{Method: "eth_chainId"}, {Method: "eth_blockNumber"}})
	if err != nil {
		if IsRateLimited(err) {
			return Unknown
		}
		return Unsupported
	}
	for _, r := range resps {
		if r.Error != nil || len(r.Result) == 0 {
			return Unsupported
		}
	}
	return Supported
}

func runBurst(ctx context.Context, c *rpc.Client, n int) *Burst {
	b := &Burst{Requests: n}
	var mu sync.Mutex
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.Call(ctx, "eth_blockNumber", nil, nil)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				b.OK++
			case IsRateLimited(err):
				b.RateLimited++
			default:
				b.Errors++
			}
		}()
	}
	wg.Wait()
	b.DurationMs = ms(time.Since(start))
	return b
}

const (
	zeroAddress = "0x0000000000000000000000000000000000000000"
	zeroHash    = "0x0000000000000000000000000000000000000000000000000000000000000000"
	futureBlock = "0xffffffffff"
)

// Quantity encodes n as a JSON-RPC hex quantity.
func Quantity(n uint64) string { return "0x" + strconv.FormatUint(n, 16) }

// ParseQuantity decodes a JSON-RPC hex quantity.
func ParseQuantity(s string) (*big.Int, error) {
	t := strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	if t == "" || t == s {
		return nil, fmt.Errorf("invalid quantity %q", s)
	}
	n, ok := new(big.Int).SetString(t, 16)
	if !ok {
		return nil, fmt.Errorf("invalid quantity %q", s)
	}
	return n, nil
}

func ms(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := p / 100 * float64(len(sorted)-1)
	lo := int(rank)
	if lo >= len(sorted)-1 {
		return sorted[len(sorted)-1]
	}
	frac := rank - float64(lo)
	return sorted[lo] + (sorted[lo+1]-sorted[lo])*frac
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
