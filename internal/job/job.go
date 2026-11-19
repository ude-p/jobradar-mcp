// Package job holds the one posting shape that every provider is mapped into.
package job

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
)

type Job struct {
	Key         string `json:"key" jsonschema:"Identity to pass to save."`
	Source      string `json:"source" jsonschema:"Provider name. Pass it with id to get_detail."`
	ID          string `json:"id" jsonschema:"Provider identifier. Pass it with source to get_detail."`
	URL         string `json:"url"`
	Title       string `json:"title"`
	Company     string `json:"company"`
	Location    string `json:"location,omitempty"`
	Posted      string `json:"posted,omitempty"`
	Remote      bool   `json:"remote,omitempty"`
	Description string `json:"description,omitempty" jsonschema:"The posting body, as the provider supplies it."`
}

// URLKey is a stable identity for a posting URL across providers, because the
// same posting arrives from a board and from an ATS with different ids.
func URLKey(raw string) string {
	raw = strings.TrimSpace(raw)
	if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
		parsed.RawQuery = ""
		parsed.Fragment = ""
		parsed.Host = strings.ToLower(parsed.Host)
		parsed.Path = strings.TrimSuffix(parsed.Path, "/")
		raw = parsed.String()
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:12])
}
