# Public RPC report, 2026-11-23

Real `rpcgauge v0.1.0` run against the built-in public presets, from a cloud VM in Europe, at 15:55 UTC on 2026-11-23. Command per chain: `rpcgauge --preset <chain> --format markdown` (defaults: 5 latency samples, logs ranges 100..100000, burst of 20). Results change with time and location; this is a snapshot, not a ranking of providers.

## ethereum

| # | ENDPOINT | CHAIN | HEAD | LAG | P50 | P95 | ARCHIVE | LOGS | DEBUG | TRACE | BATCH | BURST | STATUS |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | drpc | ethereum | 26478863 | 0 | 48ms | 50ms | yes | 100 | yes | yes | yes | 20/20 | ok |
| 2 | flashbots | ethereum | 26478863 | 0 | 49ms | 49ms | yes | 2k | no | no | yes | 20/20 | ok |
| 3 | 1rpc | ethereum | 26478863 | 0 | 73ms | 81ms | no | 50 | no | no | yes | 20/20 | ok |
| 4 | publicnode | ethereum | 26478863 | 0 | 115ms | 116ms | no | 5k | no | no | yes | 20/20 | ok |
| 5 | blastapi | ethereum | 26478863 | 0 | 124ms | 130ms | yes | 10 | no | no | yes | 20/20 | ok |
| 6 | mevblocker | ethereum | 26478863 | 0 | 191ms | 208ms | yes | 10k | no | no | yes | 20/20 | ok |
| - | merkle | - | - | - | - | - | - | - | - | - | - | - | fail |

- merkle: FAIL unreachable: eth_chainId: rpc error -32005: Rate limit exceeded

## base

| # | ENDPOINT | CHAIN | HEAD | LAG | P50 | P95 | ARCHIVE | LOGS | DEBUG | TRACE | BATCH | BURST | STATUS |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | drpc | base | 61294786 | 0 | 47ms | 52ms | yes | 100 | yes | yes | yes | 20/20 | ok |
| 2 | base.org | base | 61294786 | 0 | 73ms | 73ms | yes | unknown | no | no | yes | 20/20 | ok |
| 3 | publicnode | base | 61294786 | 0 | 111ms | 227ms | no | 50k | no | no | yes | 20/20 | ok |
| 4 | meowrpc | base | 61294786 | 0 | 377ms | 377ms | yes | no | no | no | unknown | 0/20 | warn |
| 5 | 1rpc | base | 61294775 | 11 | 91ms | 128ms | yes | 50 | no | no | yes | 20/20 | fail |

- meowrpc: WARN 4 of 5 latency samples failed
- meowrpc: WARN rate limited on 20 of 20 burst requests
- 1rpc: FAIL 11 blocks behind (max 5)

## arbitrum

| # | ENDPOINT | CHAIN | HEAD | LAG | P50 | P95 | ARCHIVE | LOGS | DEBUG | TRACE | BATCH | BURST | STATUS |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | drpc | arbitrum | 526851998 | 0 | 45ms | 49ms | no | 100 | yes | no | yes | 20/20 | ok |
| 2 | publicnode | arbitrum | 526851998 | 0 | 120ms | 124ms | no | no | no | no | yes | 20/20 | ok |
| 3 | 1rpc | arbitrum | 526851996 | 2 | 73ms | 75ms | no | 50 | no | no | yes | 20/20 | ok |
| 4 | arbitrum.io | arbitrum | 526851996 | 2 | 108ms | 172ms | no | >=100k | no | no | yes | 20/20 | ok |

## optimism

| # | ENDPOINT | CHAIN | HEAD | LAG | P50 | P95 | ARCHIVE | LOGS | DEBUG | TRACE | BATCH | BURST | STATUS |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | drpc | optimism | 159925281 | 0 | 49ms | 54ms | yes | 100 | yes | yes | yes | 20/20 | ok |
| 2 | publicnode | optimism | 159925281 | 0 | 116ms | 121ms | no | 5k | no | no | yes | 20/20 | ok |
| 3 | optimism.io | optimism | 159925281 | 0 | 177ms | 178ms | yes | 10k | no | no | yes | 20/20 | ok |
| 4 | 1rpc | optimism | 159925279 | 2 | 74ms | 85ms | yes | 50 | no | no | yes | 1/20 | warn |

- 1rpc: WARN 19 of 20 burst requests failed

## polygon

| # | ENDPOINT | CHAIN | HEAD | LAG | P50 | P95 | ARCHIVE | LOGS | DEBUG | TRACE | BATCH | BURST | STATUS |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | drpc | polygon | 97829255 | 0 | 44ms | 49ms | yes | 100 | yes | no | yes | 20/20 | ok |
| 2 | publicnode | polygon | 97829255 | 0 | 115ms | 121ms | no | 10k | no | no | yes | 20/20 | ok |
| 3 | 1rpc | polygon | 97829254 | 1 | 83ms | 88ms | no | no | no | no | yes | 20/20 | ok |
