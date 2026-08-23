package provider

import (
	"net/http"
	"strings"
)

// IsCloudflareChallengeResponse checks the documented Cloudflare challenge
// header before falling back to challenge-document markers. A generic product
// API 403 has neither signal and must not invalidate a working clearance.
func IsCloudflareChallengeResponse(header http.Header, body []byte) bool {
	if strings.EqualFold(strings.TrimSpace(header.Get("cf-mitigated")), "challenge") {
		return true
	}
	return IsCloudflareChallengeBody(body)
}

// IsCloudflareChallengeBody identifies the small set of stable markers that
// Cloudflare challenge documents expose. A JSON 403 from a product API is not
// a challenge merely because it is forbidden; keeping this check explicit
// prevents background quota probes from invalidating a good clearance.
func IsCloudflareChallengeBody(body []byte) bool {
	text := strings.ToLower(strings.TrimSpace(string(body)))
	if text == "" {
		return false
	}
	for _, marker := range []string{
		"just a moment",
		"challenge-platform",
		"__cf_chl",
		"cf-chl-",
		"cf-chl_",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
