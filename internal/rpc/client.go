// Package rpc is a minimal JSON-RPC 2.0 client over HTTP, tuned for probing:
// every call reports its round-trip time and errors keep their HTTP status
// or JSON-RPC code so callers can classify them.
package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

// Client sends JSON-RPC requests to one endpoint.
type Client struct {
	URL     string
	Headers map[string]string
	HTTP    *http.Client
	nextID  atomic.Uint64
}

// New returns a client with a dedicated keep-alive transport, so latency
// samples after the first request measure request round trips rather than
// TCP and TLS handshakes.
func New(url string, headers map[string]string, timeout time.Duration) *Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxIdleConnsPerHost = 64
	return &Client{
		URL:     url,
		Headers: headers,
		HTTP:    &http.Client{Timeout: timeout, Transport: tr},
	}
}

// Request is one JSON-RPC call.
type Request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

// Response is one JSON-RPC reply.
type Response struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *Error          `json:"error"`
}

// Error is a JSON-RPC error object.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string { return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message) }

// HTTPError is a non-2xx HTTP reply that did not carry a JSON-RPC error.
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("http %d", e.Status)
	}
	return fmt.Sprintf("http %d: %s", e.Status, e.Body)
}

func (c *Client) newRequest(method string, params []any) Request {
	if params == nil {
		params = []any{}
	}
	return Request{JSONRPC: "2.0", ID: c.nextID.Add(1), Method: method, Params: params}
}

func (c *Client) post(ctx context.Context, payload any) ([]byte, time.Duration, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "rpcgauge")
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}
	start := time.Now()
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, time.Since(start), err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	elapsed := time.Since(start)
	if err != nil {
		return nil, elapsed, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Many providers answer errors with a JSON-RPC body and a 4xx/5xx
		// status; prefer the JSON-RPC error when there is one.
		var r Response
		if json.Unmarshal(data, &r) == nil && r.Error != nil {
			return nil, elapsed, &statusRPCError{RPC: r.Error, Status: resp.StatusCode}
		}
		return nil, elapsed, &HTTPError{Status: resp.StatusCode, Body: summarizeBody(data)}
	}
	return data, elapsed, nil
}

// statusRPCError keeps the HTTP status alongside a JSON-RPC error.
type statusRPCError struct {
	RPC    *Error
	Status int
}

func (e *statusRPCError) Error() string { return e.RPC.Error() }
func (e *statusRPCError) Unwrap() error { return e.RPC }

// HTTPStatus returns the HTTP status attached to err, or 0.
func HTTPStatus(err error) int {
	switch e := err.(type) {
	case *HTTPError:
		return e.Status
	case *statusRPCError:
		return e.Status
	}
	return 0
}

// Call sends one request and decodes its result into out (if non-nil).
func (c *Client) Call(ctx context.Context, method string, params []any, out any) (time.Duration, error) {
	data, elapsed, err := c.post(ctx, c.newRequest(method, params))
	if err != nil {
		return elapsed, err
	}
	var r Response
	if err := json.Unmarshal(data, &r); err != nil {
		return elapsed, fmt.Errorf("invalid JSON-RPC response: %s", truncate(string(data), 120))
	}
	if r.Error != nil {
		return elapsed, r.Error
	}
	if out != nil {
		if err := json.Unmarshal(r.Result, out); err != nil {
			return elapsed, fmt.Errorf("decode %s result: %w", method, err)
		}
	}
	return elapsed, nil
}

// Call is a single entry of a batch.
type Call struct {
	Method string
	Params []any
}

// Batch sends calls as one JSON-RPC batch and returns responses in call order.
func (c *Client) Batch(ctx context.Context, calls []Call) ([]Response, time.Duration, error) {
	reqs := make([]Request, len(calls))
	index := map[string]int{}
	for i, call := range calls {
		reqs[i] = c.newRequest(call.Method, call.Params)
		index[fmt.Sprint(reqs[i].ID)] = i
	}
	data, elapsed, err := c.post(ctx, reqs)
	if err != nil {
		return nil, elapsed, err
	}
	var raw []Response
	if err := json.Unmarshal(data, &raw); err != nil {
		var single Response
		if json.Unmarshal(data, &single) == nil && single.Error != nil {
			return nil, elapsed, single.Error
		}
		return nil, elapsed, fmt.Errorf("batch not supported: %s", truncate(string(data), 120))
	}
	out := make([]Response, len(calls))
	for _, r := range raw {
		if i, ok := index[strings.Trim(string(r.ID), `"`)]; ok {
			out[i] = r
		}
	}
	return out, elapsed, nil
}

var titleRe = regexp.MustCompile(`(?is)<title>(.*?)</title>`)

// summarizeBody shortens an error body for display: the <title> of an HTML
// page, otherwise the text with whitespace collapsed.
func summarizeBody(data []byte) string {
	if m := titleRe.FindSubmatch(data); m != nil {
		return strings.TrimSpace(string(m[1]))
	}
	return truncate(strings.Join(strings.Fields(string(data)), " "), 200)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
