# rpcgauge

Health-check and rank EVM JSON-RPC endpoints from the command line or CI.

`rpcgauge` takes a list of RPC URLs and, for each one, measures latency, compares its head block against the other endpoints on the same chain, checks the chain id, and probes the features that differ most between providers: archive state, `eth_getLogs` block-range limits, `debug_*` / `trace_*` methods, JSON-RPC batching and rate limits. It prints a ranked table, Markdown or JSON, and exits non-zero when an endpoint fails your criteria.

```console
$ rpcgauge --preset ethereum
#  ENDPOINT    CHAIN     HEAD      LAG  P50    P95    ARCHIVE  LOGS  DEBUG  TRACE  BATCH  BURST  STATUS
1  drpc        ethereum  26478863  0    48ms   50ms   yes      100   yes    yes    yes    20/20  ok
2  flashbots   ethereum  26478863  0    49ms   49ms   yes      2k    no     no     yes    20/20  ok
3  1rpc        ethereum  26478863  0    73ms   81ms   no       50    no     no     yes    20/20  ok
4  publicnode  ethereum  26478863  0    115ms  116ms  no       5k    no     no     yes    20/20  ok
5  blastapi    ethereum  26478863  0    124ms  130ms  yes      10    no     no     yes    20/20  ok
6  mevblocker  ethereum  26478863  0    191ms  208ms  yes      10k   no     no     yes    20/20  ok
-  merkle      -         -         -    -      -      -        -     -      -      -      -      fail

merkle: FAIL unreachable: eth_chainId: rpc error -32005: Rate limit exceeded
```

A full run across Ethereum, Base, Arbitrum, Optimism and Polygon public endpoints is in [docs/public-rpc-report-2026-11-23.md](docs/public-rpc-report-2026-11-23.md).

## Install

Download a binary for Linux, macOS or Windows from the [releases page](https://github.com/piolpro/rpcgauge/releases), or build from source with Go 1.23+:

```sh
go install github.com/piolpro/rpcgauge@latest
```

The tool has no dependencies outside the Go standard library.

## Usage

```sh
# Endpoints as arguments
rpcgauge https://mainnet.base.org https://base-rpc.publicnode.com

# A built-in list of public endpoints (see --list-presets)
rpcgauge --preset arbitrum --preset optimism

# Your own list, gated for CI
rpcgauge -f endpoints.txt --chain-id 1 --require archive,trace,logs=10000 --format json
```

An endpoint file has one URL per line, optionally followed by a display name. Blank lines and `#` comments are ignored, and `${VAR}` is expanded from the environment, so API keys stay out of the file:

```text
# endpoints.txt
https://eth-mainnet.g.alchemy.com/v2/${ALCHEMY_KEY} alchemy
https://mainnet.infura.io/v3/${INFURA_KEY}         infura
http://10.0.0.5:8545                               own-node
```

Use `-f -` to read the list from stdin, and `-H 'Name: value'` (repeatable, also `${VAR}`-expanded) for providers that authenticate with headers.

Presets: `ethereum`, `base`, `arbitrum`, `optimism`, `polygon`, `bsc`, `gnosis`, `avalanche`, `sepolia`. They contain keyless public endpoints only.

## What it checks

| Column | How it is measured |
| --- | --- |
| CHAIN | `eth_chainId`, named for common chains. `--chain-id N` fails any endpoint on another chain. |
| HEAD, LAG | `eth_blockNumber`. Lag is blocks behind the highest head among endpoints on the same chain; heads are re-read together at the end so slow probes are not penalised. `--max-lag` (default 5) fails endpoints further behind. |
| P50, P95 | Latency of `--samples` (default 5) sequential `eth_blockNumber` calls over a kept-alive connection. `--max-latency 300ms` fails slow endpoints. |
| ARCHIVE | `eth_getBalance` at `--archive-block` (default 1). `yes` when historical state is served, `no` when the node reports pruned/missing state. |
| LOGS | Largest `eth_getLogs` block range that succeeded, from `--logs-ranges` (default 100,1000,5000,10000,50000,100000). If the provider names its limit in the error ("limit of 10000 blocks", "up to a 2K block range"), that value is used. `>=100k` means every tried range worked. |
| DEBUG, TRACE | Whether any `debug_*` / `trace_*` method in `--methods` exists. Calls use a zero tx hash or a future block, so they are cheap and an "invalid params" or "not found" answer still proves the method is there. |
| BATCH | Whether a JSON-RPC batch of two calls is answered. |
| BURST | `--burst` (default 20) concurrent `eth_blockNumber` calls; `ok/total`. HTTP 429 or rate-limit errors are reported as a warning. |
| STATUS | `ok`, `warn` (degraded: failed samples, rate limited) or `fail` (unreachable, wrong chain, lagging, too slow, or missing a `--require` feature). |

Endpoints are ranked by status, then lag, then p50 latency, separately per chain.

Each probed method's detailed result (including per-range timing and error text) is in the JSON output.

## CI

`--require` takes a comma-separated list of `archive`, `batch`, `debug`, `trace`, `logs=<blocks>` and any method name, e.g. `eth_getBlockReceipts`. The exit code is 0 when everything passes, 1 when any endpoint reaches the `--fail-on` level (`fail` by default, or `warn`, or `never`), and 2 for usage errors.

GitHub Actions:

```yaml
- uses: piolpro/rpcgauge@v0.1.0
  with:
    args: -f endpoints.txt --chain-id 8453 --require archive,logs=10000
  env:
    ALCHEMY_KEY: ${{ secrets.ALCHEMY_KEY }}
```

The action writes a Markdown table to the job summary, exposes the JSON report path as `steps.<id>.outputs.report`, and fails the step on the same exit codes as the CLI.

JSON output, abridged:

```json
{
  "tool": "rpcgauge",
  "version": "v0.1.0",
  "status": "ok",
  "endpoints": [
    {
      "rank": 1,
      "name": "drpc",
      "url": "base.drpc.org",
      "status": "ok",
      "chain_id": 8453,
      "head": 61294786,
      "lag": 0,
      "latency": { "samples": 5, "errors": 0, "p50_ms": 47.2, "p95_ms": 52.1 },
      "archive": "yes",
      "logs_max_range": 100,
      "methods": [{ "method": "debug_traceTransaction", "support": "yes" }],
      "batch": "yes",
      "burst": { "requests": 20, "ok": 20, "rate_limited": 0 }
    }
  ]
}
```

## Privacy

Output never prints full URLs by default: credentials, query strings and long key-like path segments (Alchemy, Infura, QuickNode style) are replaced with `***`. Pass `--show-urls` to print them as given.

## Caveats

- Public endpoints change behaviour by region, time and load; treat a single run as a snapshot.
- The burst test sends real traffic. Keep `--burst` modest against providers you do not run, or set it to 0.
- `unknown` means the endpoint answered in a way that neither proves nor rules out support (for example a timeout or an unexpected error); the detail is in the JSON.

## Development

```sh
go test -race ./...
```

Tests run against an in-process fake node (`internal/testnode`) that can simulate pruned state, log-range limits, missing methods, no batching, rate limiting and outages, so they need no network.

## License

MIT
