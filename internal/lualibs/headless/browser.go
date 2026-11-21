package headless

import (
	"net/http"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"
	"github.com/kaizen-architecture/kaizen-mangal/network"
	lua "github.com/yuin/gopher-lua"
)

var browserMethods = map[string]lua.LGFunction{
	"page":         browserPage,
	"close":        browserClose,
	"sync_cookies": browserSyncCookies,
}

func newBrowser() lua.LGFunction {
	return func(L *lua.LState) int {
		mb, err := OpenBrowser(DefaultTimeout())
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}

		ud := L.NewUserData()
		ud.Value = mb
		L.SetMetatable(ud, L.GetTypeMetatable("browser"))

		L.Push(ud)
		L.Push(lua.LNil)
		return 2
	}
}

func registerBrowserType(L *lua.LState) {
	mt := L.NewTypeMetatable("browser")
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), browserMethods))
}

func checkBrowser(L *lua.LState) *ManagedBrowser {
	ud := L.CheckUserData(1)
	if v, ok := ud.Value.(*ManagedBrowser); ok {
		return v
	}

	L.ArgError(1, "browser expected")
	return nil
}

type managedPage struct {
	page    *rod.Page
	timeout time.Duration
}

func browserPage(L *lua.LState) int {
	mb := checkBrowser(L)
	targetURL := L.ToString(2)

	p, err := stealth.Page(mb.Browser)
	if err != nil {
		L.RaiseError("headless page creation failed: %s", err.Error())
		return 0
	}

	if targetURL != "" {
		if err := p.Timeout(mb.Timeout).Navigate(targetURL); err != nil {
			_ = p.Close()
			L.RaiseError("headless navigate to %s failed: %s", targetURL, err.Error())
			return 0
		}
	}

	ud := L.NewUserData()
	ud.Value = &managedPage{page: p, timeout: mb.Timeout}
	L.SetMetatable(ud, L.GetTypeMetatable("browserPage"))

	L.Push(ud)
	return 1
}

func browserClose(L *lua.LState) int {
	mb := checkBrowser(L)
	if err := mb.Close(); err != nil {
		L.Push(lua.LString(err.Error()))
		return 1
	}
	L.Push(lua.LNil)
	return 1
}

// browserSyncCookies exports all browser cookies + navigator.userAgent into network.SharedCookieJar().
func browserSyncCookies(L *lua.LState) int {
	mb := checkBrowser(L)
	targetURL := L.OptString(2, "")

	rodCookies, err := mb.Browser.GetCookies()
	if err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	var ua string
	if pages, perr := mb.Browser.Pages(); perr == nil && len(pages) > 0 {
		if v, eerr := pages[0].Timeout(mb.Timeout).Eval(`() => navigator.userAgent`); eerr == nil {
			ua = v.Value.Str()
		}
		if targetURL == "" {
			if info, ierr := pages[0].Info(); ierr == nil && info != nil {
				targetURL = info.URL
			}
		}
	}

	stdCookies := rodCookiesToHTTP(rodCookies)
	if targetURL != "" {
		if err := network.SharedCookieJar().SetSolvedSession(targetURL, stdCookies, ua); err != nil {
			L.Push(lua.LFalse)
			L.Push(lua.LString(err.Error()))
			return 2
		}
	}

	L.Push(lua.LTrue)
	L.Push(lua.LNil)
	return 2
}

func rodCookiesToHTTP(in []*proto.NetworkCookie) []*http.Cookie {
	out := make([]*http.Cookie, 0, len(in))
	for _, rc := range in {
		var exp time.Time
		if rc.Expires > 0 {
			exp = time.Unix(int64(rc.Expires), 0)
		}
		out = append(out, &http.Cookie{
			Name:     rc.Name,
			Value:    rc.Value,
			Domain:   rc.Domain,
			Path:     rc.Path,
			Expires:  exp,
			Secure:   rc.Secure,
			HttpOnly: rc.HTTPOnly,
		})
	}
	return out
}
