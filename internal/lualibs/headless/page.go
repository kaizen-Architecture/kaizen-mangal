package headless

import (
	"time"

	"github.com/go-rod/rod"
	lua "github.com/yuin/gopher-lua"
)

var pageMethods = map[string]lua.LGFunction{
	"waitLoad":             pageWaitLoad,
	"element":              pageElement,
	"elementR":             pageElementR,
	"elements":             pageElements,
	"elementByJS":          pageElementByJS,
	"waitElementsMoreThan": pageWaitElementsMoreThan,
	"navigate":             pageNavigate,
	"has":                  pageHas,
	"eval":                 pageEval,
	"html":                 pageHTML,
	"close":                pageClose,
	"cookies":              pageCookies,
}

func registerPageType(L *lua.LState) {
	mt := L.NewTypeMetatable("browserPage")
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), pageMethods))
}

func checkPage(L *lua.LState) *managedPage {
	ud := L.CheckUserData(1)
	if v, ok := ud.Value.(*managedPage); ok {
		return v
	}
	L.ArgError(1, "browserPage expected")
	return nil
}

func (mp *managedPage) withTimeout() *rod.Page {
	d := mp.timeout
	if d <= 0 {
		d = DefaultTimeout()
	}
	return mp.page.Timeout(d)
}

func pageWaitLoad(L *lua.LState) int {
	mp := checkPage(L)
	if err := mp.withTimeout().WaitLoad(); err != nil {
		L.RaiseError("headless waitLoad failed: %s", err.Error())
	}
	return 0
}

func pageElement(L *lua.LState) int {
	mp := checkPage(L)
	selector := L.CheckString(2)

	el, err := mp.withTimeout().Element(selector)
	if err != nil {
		L.RaiseError("headless element(%q) failed: %s", selector, err.Error())
		return 0
	}

	ud := L.NewUserData()
	ud.Value = el
	L.SetMetatable(ud, L.GetTypeMetatable("pageElement"))

	L.Push(ud)
	return 1
}

func pageNavigate(L *lua.LState) int {
	mp := checkPage(L)
	targetURL := L.CheckString(2)
	if err := mp.withTimeout().Navigate(targetURL); err != nil {
		L.RaiseError("headless navigate(%q) failed: %s", targetURL, err.Error())
	}
	return 0
}

func pageElementByJS(L *lua.LState) int {
	mp := checkPage(L)
	js := L.CheckString(2)

	el, err := mp.withTimeout().ElementByJS(rod.Eval(js))
	if err != nil {
		L.RaiseError("headless elementByJS failed: %s", err.Error())
		return 0
	}

	ud := L.NewUserData()
	ud.Value = el
	L.SetMetatable(ud, L.GetTypeMetatable("pageElement"))

	L.Push(ud)
	return 1
}

func pageElements(L *lua.LState) int {
	mp := checkPage(L)
	selector := L.CheckString(2)

	els, err := mp.withTimeout().Elements(selector)
	if err != nil {
		L.RaiseError("headless elements(%q) failed: %s", selector, err.Error())
		return 0
	}

	table := L.NewTable()
	for i, el := range els {
		ud := L.NewUserData()
		ud.Value = el
		L.SetMetatable(ud, L.GetTypeMetatable("pageElement"))
		table.RawSetInt(i+1, ud)
	}

	L.Push(table)
	return 1
}

func pageHas(L *lua.LState) int {
	mp := checkPage(L)
	selector := L.CheckString(2)

	has, _, err := mp.withTimeout().Has(selector)
	if err != nil {
		L.RaiseError("headless has(%q) failed: %s", selector, err.Error())
		return 0
	}
	L.Push(lua.LBool(has))
	return 1
}

func pageEval(L *lua.LState) int {
	mp := checkPage(L)
	js := L.CheckString(2)

	res, err := mp.withTimeout().Eval(js)
	if err != nil {
		L.RaiseError("headless eval failed: %s", err.Error())
		return 0
	}
	L.Push(lua.LString(res.Value.Str()))
	return 1
}

func pageHTML(L *lua.LState) int {
	mp := checkPage(L)
	html, err := mp.withTimeout().HTML()
	if err != nil {
		L.RaiseError("headless html failed: %s", err.Error())
		return 0
	}
	L.Push(lua.LString(html))
	return 1
}

func pageElementR(L *lua.LState) int {
	mp := checkPage(L)
	selector := L.CheckString(2)
	re := L.CheckString(3)

	el, err := mp.withTimeout().ElementR(selector, re)
	if err != nil {
		L.RaiseError("headless elementR(%q, %q) failed: %s", selector, re, err.Error())
		return 0
	}

	ud := L.NewUserData()
	ud.Value = el
	L.SetMetatable(ud, L.GetTypeMetatable("pageElement"))

	L.Push(ud)
	return 1
}

func pageWaitElementsMoreThan(L *lua.LState) int {
	mp := checkPage(L)
	selector := L.CheckString(2)
	count := L.CheckInt(3)

	d := mp.timeout
	if d <= 0 {
		d = DefaultTimeout()
	}
	deadline := time.Now().Add(d)
	for {
		els, err := mp.page.Timeout(5 * time.Second).Elements(selector)
		if err == nil && len(els) > count {
			return 0
		}
		if time.Now().After(deadline) {
			L.RaiseError("timeout waiting for more than %d elements matching %q", count, selector)
			return 0
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func pageClose(L *lua.LState) int {
	mp := checkPage(L)
	if err := mp.page.Close(); err != nil {
		L.Push(lua.LString(err.Error()))
		return 1
	}
	L.Push(lua.LNil)
	return 1
}

func pageCookies(L *lua.LState) int {
	mp := checkPage(L)
	cookies, err := mp.withTimeout().Cookies(nil)
	if err != nil {
		L.RaiseError("headless cookies failed: %s", err.Error())
		return 0
	}

	tbl := L.NewTable()
	for i, c := range cookies {
		row := L.NewTable()
		row.RawSetString("name", lua.LString(c.Name))
		row.RawSetString("value", lua.LString(c.Value))
		row.RawSetString("domain", lua.LString(c.Domain))
		row.RawSetString("path", lua.LString(c.Path))
		tbl.RawSetInt(i+1, row)
	}
	L.Push(tbl)
	return 1
}
