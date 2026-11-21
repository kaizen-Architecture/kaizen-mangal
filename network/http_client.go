package network

import (
	"net/http"
	"time"
)

// HTTPClient is the swappable HTTP client abstraction for kaizen-mangal (RFC 0001 Pillar B).
// Callers depend on this interface rather than a specific TLS library.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
	SetProxy(proxyURL string) error
	SetTimeout(d time.Duration)
	SetInsecureSkipVerify(skip bool)
	Jar() http.CookieJar
}

// ClientOptions configures a newly created HTTPClient.
type ClientOptions struct {
	Profile            string
	Timeout            time.Duration
	ProxyURL           string
	InsecureSkipVerify bool
	Jar                http.CookieJar
	DisablePoliteness  bool
}
