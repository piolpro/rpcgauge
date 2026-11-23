// Command rpcgauge checks EVM JSON-RPC endpoints for latency, block lag,
// chain id, archive state, eth_getLogs range limits, debug/trace support,
// batching and rate limits, then prints a ranked table, Markdown or JSON.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/piolpro/rpcgauge/internal/presets"
	"github.com/piolpro/rpcgauge/internal/probe"
	"github.com/piolpro/rpcgauge/internal/report"
)

var version = "dev"

const (
	exitOK    = 0
	exitFail  = 1
	exitUsage = 2
)

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("rpcgauge", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		files, presetNames, headers multiFlag
		format                      = fs.String("format", "table", "output format: table, markdown or json")
		timeout                     = fs.Duration("timeout", 10*time.Second, "timeout per request")
		samples                     = fs.Int("samples", 5, "eth_blockNumber calls used to measure latency")
		logRanges                   = fs.String("logs-ranges", "100,1000,5000,10000,50000,100000", "eth_getLogs block ranges to try, ascending; empty to skip")
		methods                     = fs.String("methods", strings.Join(probe.DefaultMethods, ","), "methods to probe for support; empty to skip")
		burst                       = fs.Int("burst", 20, "concurrent requests fired to detect rate limiting; 0 to skip")
		archiveBlock                = fs.Uint64("archive-block", 1, "historical block used to test archive state")
		chainID                     = fs.Uint64("chain-id", 0, "fail endpoints that do not report this chain id")
		maxLag                      = fs.Int64("max-lag", 5, "fail endpoints more than this many blocks behind the best; -1 to disable")
		maxLatency                  = fs.Duration("max-latency", 0, "fail endpoints whose p50 latency is above this; 0 to disable")
		require                     = fs.String("require", "", "features every endpoint must have, e.g. archive,trace,debug,batch,logs=10000,eth_getBlockReceipts")
		failOn                      = fs.String("fail-on", "fail", "exit 1 when any endpoint is at this level: fail, warn or never")
		concurrency                 = fs.Int("concurrency", 8, "endpoints probed in parallel")
		showURLs                    = fs.Bool("show-urls", false, "print full endpoint URLs instead of redacting likely API keys")
		listPresets                 = fs.Bool("list-presets", false, "list built-in endpoint presets and exit")
		showVersion                 = fs.Bool("version", false, "print version and exit")
	)
	fs.Var(&files, "f", "file with one endpoint per line (`path`, - for stdin); repeatable")
	fs.Var(&presetNames, "preset", "built-in list of public endpoints (`name`, see --list-presets); repeatable")
	fs.Var(&headers, "H", "extra HTTP header sent to every endpoint (`Name: value`); repeatable")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: rpcgauge [flags] [url [url...]]\n\n")
		fmt.Fprintf(stderr, "Check and rank EVM JSON-RPC endpoints.\n\nExamples:\n")
		fmt.Fprintf(stderr, "  rpcgauge --preset ethereum\n")
		fmt.Fprintf(stderr, "  rpcgauge -f endpoints.txt --require archive,logs=10000 --format json\n")
		fmt.Fprintf(stderr, "  rpcgauge https://mainnet.base.org https://base-rpc.publicnode.com\n\nFlags:\n")
		fs.PrintDefaults()
	}
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if *showVersion {
		fmt.Fprintln(stdout, "rpcgauge", buildVersion())
		return exitOK
	}
	if *listPresets {
		for _, n := range presets.Names() {
			text, _ := presets.Get(n)
			eps, _ := parseEndpoints(text, n)
			fmt.Fprintf(stdout, "%-10s %d endpoints\n", n, len(eps))
		}
		return exitOK
	}

	usage := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "rpcgauge: "+format+"\n", a...)
		return exitUsage
	}
	switch *format {
	case "table", "markdown", "md", "json":
	default:
		return usage("unknown --format %q", *format)
	}
	switch *failOn {
	case "fail", "warn", "never":
	default:
		return usage("unknown --fail-on %q", *failOn)
	}
	reqs, err := report.ParseRequirements(*require)
	if err != nil {
		return usage("%v", err)
	}
	ranges, err := parseUintList(*logRanges)
	if err != nil {
		return usage("--logs-ranges: %v", err)
	}
	hdrs := map[string]string{}
	for _, h := range headers {
		k, v, ok := strings.Cut(h, ":")
		if !ok || strings.TrimSpace(k) == "" {
			return usage("invalid header %q, want 'Name: value'", h)
		}
		hdrs[strings.TrimSpace(k)] = os.Expand(strings.TrimSpace(v), os.Getenv)
	}

	var eps []endpoint
	for _, p := range presetNames {
		text, err := presets.Get(p)
		if err != nil {
			return usage("%v", err)
		}
		list, err := parseEndpoints(text, "preset "+p)
		if err != nil {
			return usage("%v", err)
		}
		eps = append(eps, list...)
	}
	for _, f := range files {
		var data []byte
		if f == "-" {
			data, err = io.ReadAll(stdin)
		} else {
			data, err = os.ReadFile(f)
		}
		if err != nil {
			return usage("%v", err)
		}
		list, err := parseEndpoints(string(data), f)
		if err != nil {
			return usage("%v", err)
		}
		eps = append(eps, list...)
	}
	for _, a := range positional {
		ep, err := newEndpoint(a, "")
		if err != nil {
			return usage("%v", err)
		}
		eps = append(eps, ep)
	}
	if len(eps) == 0 {
		fs.Usage()
		return exitUsage
	}

	opt := probe.Options{
		Timeout:      *timeout,
		Samples:      *samples,
		LogRanges:    ranges,
		Methods:      splitList(*methods),
		Burst:        *burst,
		ArchiveBlock: *archiveBlock,
		Headers:      hdrs,
	}
	results := probeAll(ctx, eps, opt, *concurrency, *showURLs)
	heads := syncHeads(ctx, results)
	entries := report.Evaluate(results, heads, report.Criteria{
		ExpectChainID: *chainID,
		MaxLag:        *maxLag,
		MaxLatencyMs:  float64(maxLatency.Milliseconds()),
		Require:       reqs,
	})

	switch *format {
	case "json":
		err = report.JSON(stdout, buildVersion(), entries)
	case "markdown", "md":
		err = report.Markdown(stdout, entries)
	default:
		err = report.Table(stdout, entries)
	}
	if err != nil {
		fmt.Fprintln(stderr, "rpcgauge:", err)
		return exitFail
	}

	worst := report.Worst(entries)
	switch {
	case *failOn == "never":
	case worst == report.Fail:
		return exitFail
	case *failOn == "warn" && worst == report.Warn:
		return exitFail
	}
	return exitOK
}

func probeAll(ctx context.Context, eps []endpoint, opt probe.Options, concurrency int, showURLs bool) []*probe.Result {
	if concurrency < 1 {
		concurrency = 1
	}
	results := make([]*probe.Result, len(eps))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, ep := range eps {
		wg.Add(1)
		go func(i int, ep endpoint) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			display := redact(ep.URL)
			if showURLs {
				display = ep.URL
			}
			name := ep.Name
			if name == "" {
				name = display
			}
			r := probe.Run(ctx, name, ep.URL, opt)
			r.URL = display
			results[i] = r
		}(i, ep)
	}
	wg.Wait()
	return results
}

// syncHeads reads every reachable endpoint's head at the same moment, so lag
// compares like with like instead of heads sampled seconds apart.
func syncHeads(ctx context.Context, results []*probe.Result) map[int]uint64 {
	heads := map[int]uint64{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i, r := range results {
		if !r.Reachable || r.Client() == nil {
			continue
		}
		wg.Add(1)
		go func(i int, r *probe.Result) {
			defer wg.Done()
			h, err := probe.Head(ctx, r.Client())
			if err != nil {
				return
			}
			mu.Lock()
			heads[i] = h
			mu.Unlock()
		}(i, r)
	}
	wg.Wait()
	return heads
}

// parseInterspersed lets flags appear after URLs, which the flag package
// alone does not allow. Everything after "--" is positional.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		if len(args) > len(rest) && args[len(args)-len(rest)-1] == "--" {
			return append(positional, rest...), nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseUintList(s string) ([]uint64, error) {
	var out []uint64
	for _, p := range splitList(s) {
		n, err := strconv.ParseUint(strings.ReplaceAll(p, "_", ""), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number %q", p)
		}
		out = append(out, n)
	}
	return out, nil
}

func buildVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return version
}
