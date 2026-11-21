package headless

import (
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	lua "github.com/yuin/gopher-lua"
)

var elementMethods = map[string]lua.LGFunction{
	"input":     elementInput,
	"click":     elementClick,
	"text":      elementText,
	"attribute": elementAttribute,
	"html":      elementHtml,
	"property":  elementProperty,
}

func registerElementType(L *lua.LState) {
	mt := L.NewTypeMetatable("pageElement")
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), elementMethods))
}

func checkElement(L *lua.LState) *rod.Element {
	ud := L.CheckUserData(1)
	if v, ok := ud.Value.(*rod.Element); ok {
		return v
	}

	L.ArgError(1, "element expected")
	return nil
}

func elementInput(L *lua.LState) int {
	el := checkElement(L)
	value := L.ToString(2)

	if err := el.Input(value); err != nil {
		L.RaiseError("headless element input failed: %s", err.Error())
	}
	return 0
}

func elementClick(L *lua.LState) int {
	el := checkElement(L)
	if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
		L.RaiseError("headless element click failed: %s", err.Error())
	}
	return 0
}

func elementText(L *lua.LState) int {
	el := checkElement(L)
	text, err := el.Text()
	if err != nil {
		L.RaiseError("headless element text failed: %s", err.Error())
		return 0
	}
	L.Push(lua.LString(text))
	return 1
}

func elementAttribute(L *lua.LState) int {
	el := checkElement(L)
	name := L.ToString(2)

	value, err := el.Attribute(name)
	if err != nil {
		L.RaiseError("headless element attribute(%q) failed: %s", name, err.Error())
		return 0
	}
	if value == nil {
		L.Push(lua.LString(""))
		return 1
	}

	L.Push(lua.LString(*value))
	return 1
}

func elementHtml(L *lua.LState) int {
	el := checkElement(L)
	html, err := el.HTML()
	if err != nil {
		L.RaiseError("headless element html failed: %s", err.Error())
		return 0
	}

	L.Push(lua.LString(html))
	return 1
}

func elementProperty(L *lua.LState) int {
	el := checkElement(L)
	name := L.ToString(2)

	value, err := el.Property(name)
	if err != nil {
		L.RaiseError("headless element property(%q) failed: %s", name, err.Error())
		return 0
	}

	L.Push(lua.LString(value.Str()))
	return 1
}
