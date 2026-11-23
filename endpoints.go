package main

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
)

type endpoint struct {
	Name string
	URL  string
}

// parseEndpoints reads one endpoint per line: "<url> [name]". Blank lines and
// lines starting with # are ignored, and ${VAR} references are expanded from
// the environment so API keys can stay in CI secrets.
func parseEndpoints(text, source string) ([]endpoint, error) {
	var out []endpoint
	sc := bufio.NewScanner(strings.NewReader(text))
	line := 0
	for sc.Scan() {
		line++
		s := strings.TrimSpace(sc.Text())
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		fields := strings.Fields(s)
		ep, err := newEndpoint(fields[0], strings.Join(fields[1:], " "))
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", source, line, err)
		}
		out = append(out, ep)
	}
	return out, sc.Err()
}

func newEndpoint(raw, name string) (endpoint, error) {
	expanded := os.Expand(raw, func(k string) string { return os.Getenv(k) })
	u, err := url.Parse(expanded)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return endpoint{}, fmt.Errorf("invalid endpoint URL %q", raw)
	}
	return endpoint{Name: name, URL: expanded}, nil
}

var secretish = regexp.MustCompile(`[A-Za-z0-9_\-]{20,}`)

// redact hides likely API keys: credentials, query strings and long opaque
// path segments.
func redact(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid url>"
	}
	u.User = nil
	if u.RawQuery != "" {
		u.RawQuery = "***"
	}
	segs := strings.Split(u.Path, "/")
	for i, s := range segs {
		if secretish.MatchString(s) {
			segs[i] = "***"
		}
	}
	u.Path = strings.Join(segs, "/")
	u.RawPath = ""
	out := u.String()
	out = strings.Replace(out, "%2A%2A%2A", "***", -1)
	return strings.TrimSuffix(strings.TrimPrefix(out, "https://"), "/")
}
