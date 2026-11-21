package network

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kaizen-architecture/kaizen-mangal/key"
	"github.com/kaizen-architecture/kaizen-mangal/where"
	"github.com/spf13/viper"
)

type persistedCookie struct {
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Domain   string    `json:"domain"`
	Path     string    `json:"path"`
	Expires  time.Time `json:"expires,omitempty"`
	Secure   bool      `json:"secure,omitempty"`
	HttpOnly bool      `json:"http_only,omitempty"`
}

type domainSessionMeta struct {
	UserAgent string    `json:"user_agent"`
	SolvedAt  time.Time `json:"solved_at"`
}

type cookieStoreData struct {
	Cookies []persistedCookie            `json:"cookies"`
	Domains map[string]domainSessionMeta `json:"domains,omitempty"`
}

// PersistentCookieJar is a cross-process, file-locked, wipe-tolerant http.CookieJar
// with per-domain User-Agent binding (RFC 0001 Pillar C.1 & Section 6.F.5).
type PersistentCookieJar struct {
	PathOverride string

	mu        sync.Mutex
	lastMtime time.Time
	lastPath  string
	data      cookieStoreData
}

// NewPersistentCookieJar creates a PersistentCookieJar (empty pathOverride uses Viper/where.Cache()).
func NewPersistentCookieJar(pathOverride string) *PersistentCookieJar {
	return &PersistentCookieJar{
		PathOverride: pathOverride,
		data: cookieStoreData{
			Domains: make(map[string]domainSessionMeta),
		},
	}
}

func (j *PersistentCookieJar) storePath() string {
	if j.PathOverride != "" {
		return j.PathOverride
	}
	if configured := strings.TrimSpace(viper.GetString(key.NetworkCookieJarPath)); configured != "" {
		return configured
	}
	return filepath.Join(where.Cache(), "kaizen-mangal-cookies.json")
}

func (j *PersistentCookieJar) lockPath(store string) string {
	return strings.TrimSuffix(store, filepath.Ext(store)) + ".lock"
}

func (j *PersistentCookieJar) reloadIfChangedLocked(store string) {
	info, err := os.Stat(store)
	if err != nil {
		// Wipe-tolerant: missing file = empty state.
		j.data = cookieStoreData{Domains: make(map[string]domainSessionMeta)}
		j.lastMtime = time.Time{}
		j.lastPath = store
		return
	}

	if j.lastPath == store && info.ModTime().Equal(j.lastMtime) {
		return
	}

	raw, err := os.ReadFile(store)
	if err != nil {
		j.data = cookieStoreData{Domains: make(map[string]domainSessionMeta)}
		j.lastMtime = time.Time{}
		j.lastPath = store
		return
	}

	var parsed cookieStoreData
	if err := json.Unmarshal(raw, &parsed); err != nil {
		j.data = cookieStoreData{Domains: make(map[string]domainSessionMeta)}
	} else {
		if parsed.Domains == nil {
			parsed.Domains = make(map[string]domainSessionMeta)
		}
		parsed.Cookies = pruneExpired(parsed.Cookies, time.Now())
		j.data = parsed
	}
	j.lastMtime = info.ModTime()
	j.lastPath = store
}

// Cookies implements http.CookieJar.
func (j *PersistentCookieJar) Cookies(u *url.URL) []*http.Cookie {
	if u == nil {
		return nil
	}
	store := j.storePath()

	j.mu.Lock()
	defer j.mu.Unlock()
	j.reloadIfChangedLocked(store)

	now := time.Now()
	host := strings.ToLower(u.Hostname())
	reqPath := u.Path
	if reqPath == "" {
		reqPath = "/"
	}
	isHTTPS := strings.EqualFold(u.Scheme, "https")

	var out []*http.Cookie
	for _, c := range j.data.Cookies {
		if !c.Expires.IsZero() && now.After(c.Expires) {
			continue
		}
		if c.Secure && !isHTTPS {
			continue
		}
		if !cookieDomainMatches(host, c.Domain) {
			continue
		}
		if !cookiePathMatches(reqPath, c.Path) {
			continue
		}
		out = append(out, &http.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Expires:  c.Expires,
			Secure:   c.Secure,
			HttpOnly: c.HttpOnly,
		})
	}
	return out
}

// SetCookies implements http.CookieJar.
func (j *PersistentCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	if u == nil || len(cookies) == 0 {
		return
	}
	_ = j.update(u, cookies, "")
}

// SetSolvedSession atomically persists solved cookies and the matching User-Agent for the URL's domain.
func (j *PersistentCookieJar) SetSolvedSession(rawURL string, cookies []*http.Cookie, userAgent string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	return j.update(u, cookies, userAgent)
}

// DomainUserAgent returns the persisted User-Agent associated with the host's registrable domain, if any.
func (j *PersistentCookieJar) DomainUserAgent(host string) string {
	if host == "" {
		return ""
	}
	store := j.storePath()
	domain := registrableDomain(strings.ToLower(host))

	j.mu.Lock()
	defer j.mu.Unlock()
	j.reloadIfChangedLocked(store)

	if meta, ok := j.data.Domains[domain]; ok {
		return meta.UserAgent
	}
	return ""
}

func (j *PersistentCookieJar) update(u *url.URL, cookies []*http.Cookie, userAgent string) error {
	store := j.storePath()
	unlock := acquireLockFile(j.lockPath(store), 3*time.Second)
	defer unlock()

	j.mu.Lock()
	defer j.mu.Unlock()

	// Always reload under lock so concurrent workers merge cleanly.
	j.lastMtime = time.Time{}
	j.reloadIfChangedLocked(store)

	now := time.Now()
	host := strings.ToLower(u.Hostname())
	domain := registrableDomain(host)

	for _, c := range cookies {
		if c == nil || c.Name == "" {
			continue
		}
		cDomain := strings.ToLower(strings.TrimSpace(c.Domain))
		if cDomain == "" {
			cDomain = host
		}
		cPath := c.Path
		if cPath == "" {
			cPath = "/"
		}
		exp := c.Expires
		if c.MaxAge < 0 {
			exp = now.Add(-time.Hour)
		} else if c.MaxAge > 0 {
			exp = now.Add(time.Duration(c.MaxAge) * time.Second)
		}

		entry := persistedCookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   cDomain,
			Path:     cPath,
			Expires:  exp,
			Secure:   c.Secure,
			HttpOnly: c.HttpOnly,
		}

		replaced := false
		for i, existing := range j.data.Cookies {
			if existing.Name == entry.Name && existing.Domain == entry.Domain && existing.Path == entry.Path {
				j.data.Cookies[i] = entry
				replaced = true
				break
			}
		}
		if !replaced {
			j.data.Cookies = append(j.data.Cookies, entry)
		}
	}

	j.data.Cookies = pruneExpired(j.data.Cookies, now)

	if userAgent != "" && domain != "" {
		if j.data.Domains == nil {
			j.data.Domains = make(map[string]domainSessionMeta)
		}
		j.data.Domains[domain] = domainSessionMeta{
			UserAgent: userAgent,
			SolvedAt:  now,
		}
	}

	if err := writeJSONAtomic(store, j.data); err != nil {
		return err
	}
	if info, err := os.Stat(store); err == nil {
		j.lastMtime = info.ModTime()
		j.lastPath = store
	}
	return nil
}

func pruneExpired(cookies []persistedCookie, now time.Time) []persistedCookie {
	out := make([]persistedCookie, 0, len(cookies))
	for _, c := range cookies {
		if !c.Expires.IsZero() && now.After(c.Expires) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func cookieDomainMatches(host, cookieDomain string) bool {
	cd := strings.ToLower(strings.TrimPrefix(cookieDomain, "."))
	if cd == "" {
		return false
	}
	if host == cd {
		return true
	}
	return strings.HasSuffix(host, "."+cd)
}

func cookiePathMatches(reqPath, cookiePath string) bool {
	if cookiePath == "" || cookiePath == "/" {
		return true
	}
	if reqPath == cookiePath {
		return true
	}
	if strings.HasPrefix(reqPath, cookiePath) {
		return strings.HasSuffix(cookiePath, "/") || reqPath[len(cookiePath)] == '/'
	}
	return false
}
