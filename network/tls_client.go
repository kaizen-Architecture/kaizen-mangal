package network

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
)

// TLSClient implements both HTTPClient and http.RoundTripper using bogdanfinn/tls-client
// (pure Go, CGO_ENABLED=0) with a clean fallback to stdlib net/http when profile is "none".
type TLSClient struct {
	mu                 sync.RWMutex
	profile            BrowserProfile
	timeout            time.Duration
	proxyURL           string
	insecureSkipVerify bool
	jar                http.CookieJar

	tlsClient tls_client.HttpClient
	stdClient *http.Client
}

// NewTLSClient creates a new TLSClient for the given options.
func NewTLSClient(opts ClientOptions) (*TLSClient, error) {
	profile, err := ResolveProfile(opts.Profile)
	if err != nil {
		return nil, err
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = time.Minute
	}

	c := &TLSClient{
		profile:            profile,
		timeout:            timeout,
		proxyURL:           opts.ProxyURL,
		insecureSkipVerify: opts.InsecureSkipVerify,
		jar:                opts.Jar,
	}
	if err := c.rebuildLocked(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *TLSClient) rebuildLocked() error {
	if !c.profile.HasTLSProfile {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.MaxIdleConns = 100
		tr.MaxIdleConnsPerHost = 100
		tr.MaxConnsPerHost = 200
		tr.IdleConnTimeout = 30 * time.Second
		tr.ResponseHeaderTimeout = 30 * time.Second
		tr.ExpectContinueTimeout = 30 * time.Second
		if c.insecureSkipVerify {
			tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		}
		if c.proxyURL != "" {
			u, err := url.Parse(c.proxyURL)
			if err != nil {
				return fmt.Errorf("invalid proxy URL %q: %w", c.proxyURL, err)
			}
			tr.Proxy = http.ProxyURL(u)
		}
		c.stdClient = &http.Client{
			Timeout:   c.timeout,
			Transport: tr,
			Jar:       c.jar,
		}
		c.tlsClient = nil
		return nil
	}

	timeoutSec := int(c.timeout.Seconds())
	if timeoutSec <= 0 {
		timeoutSec = 60
	}

	options := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(timeoutSec),
		tls_client.WithClientProfile(c.profile.TLSProfile),
		tls_client.WithNotFollowRedirects(),
	}
	if c.insecureSkipVerify {
		options = append(options, tls_client.WithInsecureSkipVerify())
	}
	if c.proxyURL != "" {
		options = append(options, tls_client.WithProxyUrl(c.proxyURL))
	}

	tc, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(), options...)
	if err != nil {
		return fmt.Errorf("failed to initialize tls-client: %w", err)
	}
	c.tlsClient = tc
	c.stdClient = nil
	return nil
}

func (c *TLSClient) SetProxy(proxyURL string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.proxyURL = proxyURL
	return c.rebuildLocked()
}

func (c *TLSClient) SetTimeout(d time.Duration) {
	if d <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.timeout = d
	_ = c.rebuildLocked()
}

func (c *TLSClient) SetInsecureSkipVerify(skip bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.insecureSkipVerify = skip
	_ = c.rebuildLocked()
}

func (c *TLSClient) Jar() http.CookieJar {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.jar
}

// Do executes req following standard redirects (up to 10) and applying profile headers.
func (c *TLSClient) Do(req *http.Request) (*http.Response, error) {
	client := &http.Client{
		Transport: c,
		Timeout:   c.getTimeout(),
		Jar:       c.Jar(),
	}
	return client.Do(req)
}

func (c *TLSClient) getTimeout() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.timeout
}

// RoundTrip implements http.RoundTripper so TLSClient plugs into stdlib http.Client and Colly.
func (c *TLSClient) RoundTrip(req *http.Request) (*http.Response, error) {
	c.mu.RLock()
	profile := c.profile
	tc := c.tlsClient
	std := c.stdClient
	jar := c.jar
	c.mu.RUnlock()

	domain := ""
	if req.URL != nil {
		domain = req.URL.Hostname()
	}

	// Fallback path when impersonate="none".
	if !profile.HasTLSProfile && std != nil {
		outReq := req.Clone(req.Context())
		applyProfileHeadersStd(outReq, profile)
		resp, err := std.Transport.RoundTrip(outReq)
		if err != nil {
			return nil, NormalizeError(err, domain, 0)
		}
		return resp, nil
	}

	freq, err := fhttp.NewRequestWithContext(req.Context(), req.Method, req.URL.String(), req.Body)
	if err != nil {
		return nil, NormalizeError(err, domain, 0)
	}
	freq.ContentLength = req.ContentLength
	freq.Host = req.Host

	for k, vals := range req.Header {
		for _, v := range vals {
			freq.Header.Add(k, v)
		}
	}

	applyProfileHeadersFHTTP(freq, profile)

	// Attach cookies from shared jar if caller hasn't already injected a Cookie header.
	if jar != nil && req.URL != nil && freq.Header.Get("Cookie") == "" {
		if cookies := jar.Cookies(req.URL); len(cookies) > 0 {
			parts := make([]string, 0, len(cookies))
			for _, ck := range cookies {
				parts = append(parts, ck.Name+"="+ck.Value)
			}
			freq.Header.Set("Cookie", strings.Join(parts, "; "))
		}
	}

	fresp, err := tc.Do(freq)
	if err != nil {
		return nil, NormalizeError(err, domain, 0)
	}

	stdResp := &http.Response{
		Status:        fresp.Status,
		StatusCode:    fresp.StatusCode,
		Proto:         fresp.Proto,
		ProtoMajor:    fresp.ProtoMajor,
		ProtoMinor:    fresp.ProtoMinor,
		Header:        make(http.Header, len(fresp.Header)),
		Body:          fresp.Body,
		ContentLength: fresp.ContentLength,
		Request:       req,
	}
	for k, vals := range fresp.Header {
		copied := make([]string, len(vals))
		copy(copied, vals)
		stdResp.Header[k] = copied
	}

	// Persist response cookies back into the shared jar.
	if jar != nil && req.URL != nil {
		if setCookies := stdResp.Cookies(); len(setCookies) > 0 {
			jar.SetCookies(req.URL, setCookies)
		}
	}

	return stdResp, nil
}

func applyProfileHeadersStd(req *http.Request, profile BrowserProfile) {
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", profile.UserAgent)
	}
	for k, v := range profile.DefaultHeaders {
		if req.Header.Get(k) == "" {
			req.Header.Set(k, v)
		}
	}
}

func applyProfileHeadersFHTTP(req *fhttp.Request, profile BrowserProfile) {
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", profile.UserAgent)
	}
	for k, v := range profile.DefaultHeaders {
		if req.Header.Get(k) == "" {
			req.Header.Set(k, v)
		}
	}
}
