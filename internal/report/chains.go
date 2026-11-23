package report

import "strconv"

var chainNames = map[uint64]string{
	1:        "ethereum",
	10:       "optimism",
	56:       "bsc",
	100:      "gnosis",
	130:      "unichain",
	137:      "polygon",
	146:      "sonic",
	250:      "fantom",
	324:      "zksync",
	1101:     "polygon-zkevm",
	1284:     "moonbeam",
	5000:     "mantle",
	8453:     "base",
	17000:    "holesky",
	34443:    "mode",
	42161:    "arbitrum",
	42170:    "arbitrum-nova",
	42220:    "celo",
	43114:    "avalanche",
	59144:    "linea",
	80094:    "berachain",
	81457:    "blast",
	84532:    "base-sepolia",
	421614:   "arbitrum-sepolia",
	534352:   "scroll",
	560048:   "hoodi",
	7777777:  "zora",
	11155111: "sepolia",
	11155420: "optimism-sepolia",
}

// ChainName returns a short name for a chain id, or the id itself.
func ChainName(id uint64) string {
	if n, ok := chainNames[id]; ok {
		return n
	}
	return strconv.FormatUint(id, 10)
}
