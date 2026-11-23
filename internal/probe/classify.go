package probe

import (
	"errors"
	"strings"

	"github.com/piolpro/rpcgauge/internal/rpc"
)

// Support is whether an endpoint serves a method.
type Support string

const (
	Supported   Support = "yes"
	Unsupported Support = "no"
	Unknown     Support = "unknown"
)

// IsRateLimited reports whether err looks like a rate-limit rejection.
func IsRateLimited(err error) bool {
	if err == nil {
		return false
	}
	if rpc.HTTPStatus(err) == 429 {
		return true
	}
	// Codes are not trusted on their own: -32005 means "limit exceeded" for
	// block ranges and result sizes too, depending on the provider.
	msg := strings.ToLower(err.Error())
	for _, p := range []string{"rate limit", "rate-limit", "ratelimit", "rate exceeded", "request rate", "too many requests", "request limit reached", "requests limit", "throttl", "compute units", "credits"} {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

// ClassifyMethodError decides whether an error returned for a method probe
// means the method is unavailable. Probes are sent with cheap or invalid
// parameters, so an error that is not about the method itself (invalid
// params, transaction not found, genesis not traceable) means it exists.
func ClassifyMethodError(err error) Support {
	if err == nil {
		return Supported
	}
	if IsRateLimited(err) {
		return Unknown
	}
	switch rpc.HTTPStatus(err) {
	case 401, 403, 404, 405, 406, 501:
		return Unsupported
	}
	var re *rpc.Error
	if errors.As(err, &re) {
		if re.Code == -32601 {
			return Unsupported
		}
		if isUnavailableMessage(re.Message) {
			return Unsupported
		}
		return Supported
	}
	var he *rpc.HTTPError
	if errors.As(err, &he) {
		if isUnavailableMessage(he.Body) {
			return Unsupported
		}
	}
	return Unknown
}

func isUnavailableMessage(msg string) bool {
	m := strings.ToLower(msg)
	if strings.Contains(m, "method") || strings.Contains(m, "namespace") || strings.Contains(m, "api") {
		for _, p := range []string{"not found", "does not exist", "not exist", "not supported", "unsupported", "not allowed", "not available", "unavailable", "disabled", "not enabled", "whitelist", "allowlist", "not permitted", "forbidden", "blocked"} {
			if strings.Contains(m, p) {
				return true
			}
		}
	}
	for _, p := range []string{"upgrade your", "upgrade to", "premium", "personal token", "api key", "request filter", "rejected due to", "paid plan", "paid tier", "your plan", "not whitelisted", "unsupported method"} {
		if strings.Contains(m, p) {
			return true
		}
	}
	return false
}

// IsMissingState reports whether err means the node lacks historical state.
func IsMissingState(err error) bool {
	if err == nil {
		return false
	}
	m := strings.ToLower(err.Error())
	for _, p := range []string{"missing trie node", "historical state", "state not available", "state is not available", "pruned", "state histories", "header not found", "unknown block", "archive", "not available"} {
		if strings.Contains(m, p) {
			return true
		}
	}
	return false
}
