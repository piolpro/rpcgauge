# Changelog

## v0.1.0 - 2026-11-23

First release.

- Probe any number of EVM JSON-RPC endpoints in parallel: chain id, client version, head block, latency (min/p50/p95/max).
- Block lag against the best endpoint on the same chain, with heads re-synced at the end of the run so slow probes are not penalised.
- Archive state check (historical `eth_getBalance`), `eth_getLogs` range limits (including limits providers advertise in their error messages), debug/trace and arbitrary method support, JSON-RPC batch support, and a concurrent burst to detect rate limiting.
- Ranked table, Markdown or JSON output; `--require`, `--chain-id`, `--max-lag`, `--max-latency` and `--fail-on` for CI gates.
- Built-in public endpoint presets for Ethereum, Base, Arbitrum, Optimism, Polygon, BNB Smart Chain, Gnosis, Avalanche and Sepolia.
- API keys in URLs are redacted in output unless `--show-urls` is set; `${ENV}` expansion in URLs and headers.
- GitHub Action wrapper (`uses: piolpro/rpcgauge@v0.1.0`).
