package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/piolpro/rpcgauge/internal/testnode"
)

func runCLI(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(context.Background(), args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestCLIEndToEnd(t *testing.T) {
	good := testnode.New(testnode.Config{ChainID: 1, Head: 1000, Archive: true, Methods: []string{"trace_block"}})
	defer good.Close()
	lagging := testnode.New(testnode.Config{ChainID: 1, Head: 980})
	defer lagging.Close()

	code, out, errOut := runCLI(t, "", good.URL, "--format", "json", "--burst", "5", lagging.URL, "--samples", "2")
	if code != exitFail {
		t.Fatalf("exit %d, want %d; stderr %s", code, exitFail, errOut)
	}
	var doc struct {
		Status    string `json:"status"`
		Endpoints []struct {
			Name    string `json:"name"`
			Status  string `json:"status"`
			Lag     int64  `json:"lag"`
			Archive string `json:"archive"`
		} `json:"endpoints"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out)
	}
	if len(doc.Endpoints) != 2 || doc.Endpoints[0].Status != "ok" || doc.Endpoints[1].Lag != 20 || doc.Endpoints[1].Status != "fail" {
		t.Errorf("unexpected report: %s", out)
	}

	code, _, _ = runCLI(t, "", good.URL, lagging.URL, "--max-lag", "-1", "--burst", "0")
	if code != exitOK {
		t.Errorf("--max-lag -1: exit %d", code)
	}
	code, _, _ = runCLI(t, "", good.URL, lagging.URL, "--fail-on", "never")
	if code != exitOK {
		t.Errorf("--fail-on never: exit %d", code)
	}
	code, out, _ = runCLI(t, good.URL+" mynode\n", "-f", "-", "--require", "archive,trace", "--format", "markdown")
	if code != exitOK || !strings.Contains(out, "| mynode |") {
		t.Errorf("stdin + require: exit %d\n%s", code, out)
	}
	code, out, _ = runCLI(t, good.URL+"\n", "-f", "-", "--require", "debug")
	if code != exitFail || !strings.Contains(out, "FAIL no debug_* methods") {
		t.Errorf("require debug: exit %d\n%s", code, out)
	}
}

func TestCLIUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"--format", "xml", "http://x"},
		{"--fail-on", "sometimes", "http://x"},
		{"--require", "logs", "http://x"},
		{"--preset", "nope"},
		{"not-a-url"},
		{"-H", "novalue", "http://x"},
	} {
		if code, _, _ := runCLI(t, "", args...); code != exitUsage {
			t.Errorf("%v: exit %d, want %d", args, code, exitUsage)
		}
	}
	if code, out, _ := runCLI(t, "", "--list-presets"); code != exitOK || !strings.Contains(out, "ethereum") {
		t.Errorf("--list-presets: %d %s", code, out)
	}
	if code, out, _ := runCLI(t, "", "--version"); code != exitOK || !strings.HasPrefix(out, "rpcgauge ") {
		t.Errorf("--version: %d %s", code, out)
	}
}

func TestParseEndpointsFile(t *testing.T) {
	t.Setenv("RPCGAUGE_TEST_KEY", "abc123")
	path := filepath.Join(t.TempDir(), "eps.txt")
	os.WriteFile(path, []byte("# comment\n\nhttps://a.example/v2/${RPCGAUGE_TEST_KEY} alchemy main\nhttp://localhost:8545\n"), 0o600)
	data, _ := os.ReadFile(path)
	eps, err := parseEndpoints(string(data), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 2 || eps[0].URL != "https://a.example/v2/abc123" || eps[0].Name != "alchemy main" || eps[1].Name != "" {
		t.Errorf("got %+v", eps)
	}
	if _, err := parseEndpoints("ftp://nope\n", "x"); err == nil || !strings.Contains(err.Error(), "x:1") {
		t.Errorf("want located error, got %v", err)
	}
}

func TestRedact(t *testing.T) {
	cases := map[string]string{
		"https://eth-mainnet.g.alchemy.com/v2/AbCdEfGhIjKlMnOpQrStUvWx": "eth-mainnet.g.alchemy.com/v2/***",
		"https://mainnet.infura.io/v3/0123456789abcdef0123456789abcdef": "mainnet.infura.io/v3/***",
		"https://rpc.example.com/?apikey=secret":                        "rpc.example.com/?***",
		"https://user:pass@node.example.com/rpc":                        "node.example.com/rpc",
		"https://mainnet.base.org":                                      "mainnet.base.org",
		"http://localhost:8545":                                         "http://localhost:8545",
	}
	for in, want := range cases {
		if got := redact(in); got != want {
			t.Errorf("redact(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPresetsParse(t *testing.T) {
	code, out, _ := runCLI(t, "", "--list-presets")
	if code != exitOK {
		t.Fatal(code)
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.HasSuffix(line, " 0 endpoints") {
			t.Errorf("empty preset: %s", line)
		}
	}
}
