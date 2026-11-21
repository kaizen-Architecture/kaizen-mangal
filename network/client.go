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

	sharedJar     = NewPersistentCookieJar("")
	sharedBreaker = &DomainCircuitBreaker{}
)

// SharedCookieJar returns the process-wide persistent cookie jar shared across
// network.Client, Colly generic scrapers, and Lua http/headless clients.
func SharedCookieJar() *PersistentCookieJar {
	return sharedJar
}

// SharedBreaker returns the process-wide per-domain challenge circuit breaker.
func SharedBreaker() *DomainCircuitBreaker {
	return sharedBreaker
}

// DefaultTransport returns the shared Challenge -> Politeness -> TLSImpersonation RoundTripper,
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
		Jar:     sharedJar,
	})
	if err != nil {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		polite := &PoliteRoundTripper{Base: tr}
		defaultTransport = &ChallengeRoundTripper{
			Base:    polite,
			Jar:     sharedJar,
			Breaker: sharedBreaker,
		}
		defaultProfile = "none"
		return defaultTransport
	}

	polite := &PoliteRoundTripper{Base: tc}
	defaultTransport = &ChallengeRoundTripper{
		Base:    polite,
		Jar:     sharedJar,
		Breaker: sharedBreaker,
	}
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
	Jar:       sharedJar,
}

// NewClient constructs a new swappable HTTPClient backed by Challenge -> Polite -> TLSClient.
func NewClient(opts ClientOptions) (HTTPClient, error) {
	if opts.Profile == "" {
		opts.Profile = ActiveProfile().Name
	}
	if opts.Jar == nil {
		opts.Jar = sharedJar
	}
	tc, err := NewTLSClient(opts)
	if err != nil {
		return nil, err
	}

	var rt http.RoundTripper = tc
	if !opts.DisablePoliteness {
		rt = &PoliteRoundTripper{Base: rt}
	}

	jar, _ := opts.Jar.(*PersistentCookieJar)
	if jar == nil {
		jar = sharedJar
	}
	rt = &ChallengeRoundTripper{
		Base:    rt,
		Jar:     jar,
		Breaker: sharedBreaker,
	}

	return &politeHTTPClient{
		inner: tc,
		rt:    rt,
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
