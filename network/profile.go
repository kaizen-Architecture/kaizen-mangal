package network

import (
	"fmt"
	"strings"

	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/kaizen-architecture/kaizen-mangal/key"
	"github.com/spf13/viper"
)

// DefaultProfileName is the pinned default browser profile chosen in Phase 3 (RFC 0001 Q1).
const DefaultProfileName = "chrome_133"

// BrowserProfile bundles a coherent TLS/HTTP2 fingerprint, User-Agent, and client hints.
type BrowserProfile struct {
	Name           string
	TLSProfile     profiles.ClientProfile
	HasTLSProfile  bool
	ChromeMajor    int
	UserAgent      string
	DefaultHeaders map[string]string
}

var pinnedProfiles = map[string]BrowserProfile{
	"chrome_133": {
		Name:          "chrome_133",
		TLSProfile:    profiles.Chrome_133,
		HasTLSProfile: true,
		ChromeMajor:   133,
		UserAgent:     "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36",
		DefaultHeaders: map[string]string{
			"Accept":             "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8",
			"Accept-Language":    "en-US,en;q=0.9",
			"Sec-Ch-Ua":          `"Not(A:Brand";v="99", "Google Chrome";v="133", "Chromium";v="133"`,
			"Sec-Ch-Ua-Mobile":   "?0",
			"Sec-Ch-Ua-Platform": `"Windows"`,
		},
	},
	"chrome_131": {
		Name:          "chrome_131",
		TLSProfile:    profiles.Chrome_131,
		HasTLSProfile: true,
		ChromeMajor:   131,
		UserAgent:     "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		DefaultHeaders: map[string]string{
			"Accept":             "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8",
			"Accept-Language":    "en-US,en;q=0.9",
			"Sec-Ch-Ua":          `"Google Chrome";v="131", "Chromium";v="131", "Not_A Brand";v="24"`,
			"Sec-Ch-Ua-Mobile":   "?0",
			"Sec-Ch-Ua-Platform": `"Windows"`,
		},
	},
	"firefox_135": {
		Name:          "firefox_135",
		TLSProfile:    profiles.Firefox_135,
		HasTLSProfile: true,
		ChromeMajor:   0,
		UserAgent:     "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:135.0) Gecko/20100101 Firefox/135.0",
		DefaultHeaders: map[string]string{
			"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
			"Accept-Language": "en-US,en;q=0.5",
		},
	},
	"firefox_133": {
		Name:          "firefox_133",
		TLSProfile:    profiles.Firefox_133,
		HasTLSProfile: true,
		ChromeMajor:   0,
		UserAgent:     "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:133.0) Gecko/20100101 Firefox/133.0",
		DefaultHeaders: map[string]string{
			"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
			"Accept-Language": "en-US,en;q=0.5",
		},
	},
	"safari_16_0": {
		Name:          "safari_16_0",
		TLSProfile:    profiles.Safari_16_0,
		HasTLSProfile: true,
		ChromeMajor:   0,
		UserAgent:     "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.0 Safari/605.1.15",
		DefaultHeaders: map[string]string{
			"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			"Accept-Language": "en-US,en;q=0.9",
		},
	},
	"none": {
		Name:          "none",
		HasTLSProfile: false,
		ChromeMajor:   0,
		UserAgent:     "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36",
		DefaultHeaders: map[string]string{
			"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			"Accept-Language": "en-US,en;q=0.9",
		},
	},
}

// ResolveProfile returns the pinned BrowserProfile for name (empty defaults to DefaultProfileName).
func ResolveProfile(name string) (BrowserProfile, error) {
	keyName := strings.ToLower(strings.TrimSpace(name))
	if keyName == "" {
		keyName = DefaultProfileName
	}
	p, ok := pinnedProfiles[keyName]
	if !ok {
		return BrowserProfile{}, fmt.Errorf("unknown network.impersonate profile %q", name)
	}
	return p, nil
}

// ActiveProfile returns the currently configured BrowserProfile from Viper (falling back to DefaultProfileName).
func ActiveProfile() BrowserProfile {
	p, err := ResolveProfile(viper.GetString(key.NetworkImpersonate))
	if err != nil {
		return pinnedProfiles[DefaultProfileName]
	}
	return p
}
