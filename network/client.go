package network

import (
	"net/http"
	"sync"
	"time"
)

var (
	defaultMu        sync.Mutex
	defaultTransport http.RoundTripper
	defaultProfile   string
)

// DefaultTransport returns the shared impersonated + polite http.RoundTripper,
// rebuilding automatically if network.impersonate changes in Viper.
func DefaultTransport() http.RoundTripper {
	active := ActiveProfile()
	defaultMu.Lock()
	defer defaultMu.Unlock()

	if defaultTransport != nil && defaultProfile == active.Name {
		return defaultTransport
	}

	tc, err := NewTLSClient(ClientOptions{
		Profile: active.Name,
		Timeout: time.Minute,
	})
	if err != nil {
		// Safe fallback to stdlib transport if profile init ever fails.
		tr := http.DefaultTransport.(*http.Transport).Clone()
		defaultTransport = &PoliteRoundTripper{Base: tr}
		defaultProfile = "none"
		return defaultTransport
	}

	defaultTransport = &PoliteRoundTripper{Base: tc}
	defaultProfile = active.Name
	return defaultTransport
}

// dynamicRoundTripper delegates to DefaultTransport() so Client always reflects
// runtime config (loaded after package init via config.Setup()).
type dynamicRoundTripper struct{}

func (dynamicRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return DefaultTransport().RoundTrip(req)
}

// Client is the shared *http.Client used by source/page.go and internal callers.
var Client = &http.Client{
	Timeout:   time.Minute,
	Transport: dynamicRoundTripper{},
}

// NewClient constructs a new swappable HTTPClient backed by TLSClient + PoliteRoundTripper.
func NewClient(opts ClientOptions) (HTTPClient, error) {
	if opts.Profile == "" {
		opts.Profile = ActiveProfile().Name
	}
	tc, err := NewTLSClient(opts)
	if err != nil {
		return nil, err
	}
	if opts.DisablePoliteness {
		return tc, nil
	}
	return &politeHTTPClient{
		inner: tc,
		rt:    &PoliteRoundTripper{Base: tc},
	}, nil
}

type politeHTTPClient struct {
	inner *TLSClient
	rt    http.RoundTripper
}

func (c *politeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	client := &http.Client{
		Transport: c.rt,
		Timeout:   c.inner.getTimeout(),
		Jar:       c.inner.Jar(),
	}
	return client.Do(req)
}

func (c *politeHTTPClient) SetProxy(proxyURL string) error {
	return c.inner.SetProxy(proxyURL)
}

func (c *politeHTTPClient) SetTimeout(d time.Duration) {
	c.inner.SetTimeout(d)
}

func (c *politeHTTPClient) SetInsecureSkipVerify(skip bool) {
	c.inner.SetInsecureSkipVerify(skip)
}

func (c *politeHTTPClient) Jar() http.CookieJar {
	return c.inner.Jar()
}
