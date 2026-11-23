// Package testnode is a configurable fake EVM JSON-RPC server for tests.
package testnode

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Config describes how the fake node behaves.
type Config struct {
	ChainID uint64
	Head    uint64
	Archive bool
	// LogsLimit is the largest eth_getLogs span accepted (0 = unlimited).
	LogsLimit uint64
	// Methods lists extra methods served beyond the core eth_* set.
	Methods []string
	NoBatch bool
	// MaxConcurrent rejects requests with HTTP 429 above this many in flight
	// (0 = unlimited).
	MaxConcurrent int
	Delay         time.Duration
	Down          bool
}

// Node is a running fake node.
type Node struct {
	*httptest.Server
	cfg      Config
	inflight atomic.Int64
	mu       sync.Mutex
	Calls    map[string]int
}

// New starts a fake node; call Close when done.
func New(cfg Config) *Node {
	n := &Node{cfg: cfg, Calls: map[string]int{}}
	n.Server = httptest.NewServer(http.HandlerFunc(n.serve))
	return n
}

type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params []json.RawMessage
}

func (n *Node) serve(w http.ResponseWriter, r *http.Request) {
	if n.cfg.Down {
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	cur := n.inflight.Add(1)
	defer n.inflight.Add(-1)
	if n.cfg.Delay > 0 {
		time.Sleep(n.cfg.Delay)
	}
	if n.cfg.MaxConcurrent > 0 && cur > int64(n.cfg.MaxConcurrent) {
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		return
	}
	body, _ := io.ReadAll(r.Body)
	w.Header().Set("Content-Type", "application/json")
	if strings.HasPrefix(strings.TrimSpace(string(body)), "[") {
		if n.cfg.NoBatch {
			w.Write([]byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"batch requests are not supported"}}`))
			return
		}
		var reqs []request
		json.Unmarshal(body, &reqs)
		out := make([]map[string]any, 0, len(reqs))
		for _, rq := range reqs {
			out = append(out, n.handle(rq))
		}
		json.NewEncoder(w).Encode(out)
		return
	}
	var rq request
	json.Unmarshal(body, &rq)
	json.NewEncoder(w).Encode(n.handle(rq))
}

func (n *Node) handle(rq request) map[string]any {
	n.mu.Lock()
	n.Calls[rq.Method]++
	n.mu.Unlock()
	res := map[string]any{"jsonrpc": "2.0", "id": rq.ID}
	fail := func(code int, msg string) map[string]any {
		res["error"] = map[string]any{"code": code, "message": msg}
		return res
	}
	switch rq.Method {
	case "eth_chainId":
		res["result"] = hex(n.cfg.ChainID)
	case "eth_blockNumber":
		res["result"] = hex(n.cfg.Head)
	case "web3_clientVersion":
		res["result"] = "FakeNode/v1.0.0"
	case "eth_getBalance":
		var block string
		if len(rq.Params) > 1 {
			json.Unmarshal(rq.Params[1], &block)
		}
		b, _ := strconv.ParseUint(strings.TrimPrefix(block, "0x"), 16, 64)
		if !n.cfg.Archive && b+128 < n.cfg.Head {
			return fail(-32000, "missing trie node 0xabc (path ) state 0xabc is not available")
		}
		res["result"] = "0x0"
	case "eth_getLogs":
		var f struct{ FromBlock, ToBlock string }
		if len(rq.Params) > 0 {
			json.Unmarshal(rq.Params[0], &f)
		}
		from, _ := strconv.ParseUint(strings.TrimPrefix(f.FromBlock, "0x"), 16, 64)
		to, _ := strconv.ParseUint(strings.TrimPrefix(f.ToBlock, "0x"), 16, 64)
		if n.cfg.LogsLimit > 0 && to-from+1 > n.cfg.LogsLimit {
			return fail(-32005, fmt.Sprintf("query exceeds max block range %d", n.cfg.LogsLimit))
		}
		res["result"] = []any{}
	default:
		for _, m := range n.cfg.Methods {
			if m == rq.Method {
				if strings.Contains(m, "Transaction") {
					return fail(-32000, "transaction 0x0000 not found")
				}
				res["result"] = nil
				return res
			}
		}
		return fail(-32601, fmt.Sprintf("the method %s does not exist/is not available", rq.Method))
	}
	return res
}

func hex(n uint64) string { return "0x" + strconv.FormatUint(n, 16) }
