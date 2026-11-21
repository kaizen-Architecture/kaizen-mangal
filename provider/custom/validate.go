package custom

import (
	"bytes"
	"errors"
	"io"
	"regexp"
	"strconv"

	"github.com/kaizen-architecture/kaizen-mangal/filesystem"
	libs "github.com/kaizen-architecture/kaizen-mangal/internal/lualibs"
	"github.com/kaizen-architecture/kaizen-mangal/util"
	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"
)

// ValidationError describes a single validation failure in a Lua source file.
type ValidationError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Line    int    `json:"line,omitempty"`
}

// ValidationResult is the machine-readable output of `kaizen-mangal sources validate`.
type ValidationResult struct {
	Valid            bool              `json:"valid"`
	Name             string            `json:"name"`
	Errors           []ValidationError `json:"errors"`
	MissingFunctions []string          `json:"missing_functions"`
	UsesHeadless     bool              `json:"uses_headless"`
}

var (
	headlessRequireRe = regexp.MustCompile(`require\s*\(?\s*["']headless["']`)
	errorLineRe       = regexp.MustCompile(`:(\d+):`)
)

// ValidateFile compiles and loads a Lua source at the exact given file path in an
// isolated Lua state with stubbed `http` and `headless` modules so top-level code
// cannot make network requests or spawn Chromium during validation.
func ValidateFile(path string) ValidationResult {
	res := ValidationResult{
		Name:             util.FileStem(path),
		Errors:           []ValidationError{},
		MissingFunctions: []string{},
	}

	file, err := filesystem.Api().Open(path)
	if err != nil {
		res.Errors = append(res.Errors, ValidationError{
			Kind:    "io",
			Message: err.Error(),
		})
		return res
	}
	raw, readErr := io.ReadAll(file)
	_ = file.Close()
	if readErr != nil {
		res.Errors = append(res.Errors, ValidationError{
			Kind:    "io",
			Message: readErr.Error(),
		})
		return res
	}

	if headlessRequireRe.Match(raw) {
		res.UsesHeadless = true
	}

	chunk, err := parse.Parse(bytes.NewReader(raw), path)
	if err != nil {
		res.Errors = append(res.Errors, ValidationError{
			Kind:    "syntax",
			Message: err.Error(),
			Line:    extractErrorLine(err),
		})
		return res
	}

	proto, err := lua.Compile(chunk, path)
	if err != nil {
		res.Errors = append(res.Errors, ValidationError{
			Kind:    "compile",
			Message: err.Error(),
			Line:    extractErrorLine(err),
		})
		return res
	}

	state := lua.NewState()
	defer state.Close()

	libs.Preload(state)
	preloadValidationStubs(state, &res.UsesHeadless)

	lfunc := state.NewFunctionFromProto(proto)
	state.Push(lfunc)
	if err := state.PCall(0, lua.MultRet, nil); err != nil {
		res.Errors = append(res.Errors, ValidationError{
			Kind:    "runtime",
			Message: err.Error(),
			Line:    extractErrorLine(err),
		})
		return res
	}

	for _, fn := range mustHave {
		defined := state.GetGlobal(fn)
		if defined.Type() != lua.LTFunction {
			res.MissingFunctions = append(res.MissingFunctions, fn)
		}
	}

	res.Valid = len(res.Errors) == 0 && len(res.MissingFunctions) == 0
	return res
}

func extractErrorLine(err error) int {
	if err == nil {
		return 0
	}
	var pe *parse.Error
	if errors.As(err, &pe) && pe.Pos.Line > 0 {
		return pe.Pos.Line
	}
	if m := errorLineRe.FindStringSubmatch(err.Error()); len(m) == 2 {
		if n, convErr := strconv.Atoi(m[1]); convErr == nil {
			return n
		}
	}
	return 0
}

func preloadValidationStubs(L *lua.LState, usesHeadless *bool) {
	// Stub `http` module: never opens sockets during validation.
	L.PreloadModule("http", func(L *lua.LState) int {
		noopErr := L.NewFunction(func(L *lua.LState) int {
			L.Push(lua.LNil)
			L.Push(lua.LString("http disabled during validation"))
			return 2
		})
		clientFn := L.NewFunction(func(L *lua.LState) int {
			cli := L.NewTable()
			L.SetField(cli, "do_request", noopErr)
			L.SetField(cli, "get", noopErr)
			L.SetField(cli, "post", noopErr)
			L.Push(cli)
			return 1
		})
		reqFn := L.NewFunction(func(L *lua.LState) int {
			req := L.NewTable()
			L.SetField(req, "header_set", L.NewFunction(func(L *lua.LState) int { return 0 }))
			L.SetField(req, "set_basic_auth", L.NewFunction(func(L *lua.LState) int { return 0 }))
			L.Push(req)
			return 1
		})
		mod := L.NewTable()
		L.SetField(mod, "client", clientFn)
		L.SetField(mod, "request", reqFn)
		L.SetField(mod, "get", noopErr)
		L.SetField(mod, "post", noopErr)
		L.SetField(mod, "put", noopErr)
		L.SetField(mod, "delete", noopErr)
		L.Push(mod)
		return 1
	})

	// Stub `headless` module: never launches Chromium during validation.
	L.PreloadModule("headless", func(L *lua.LState) int {
		*usesHeadless = true
		noopZero := L.NewFunction(func(L *lua.LState) int { return 0 })
		noopEmptyStr := L.NewFunction(func(L *lua.LState) int {
			L.Push(lua.LString(""))
			return 1
		})
		noopEmptyTbl := L.NewFunction(func(L *lua.LState) int {
			L.Push(L.NewTable())
			return 1
		})
		pageFn := L.NewFunction(func(L *lua.LState) int {
			p := L.NewTable()
			L.SetField(p, "navigate", noopZero)
			L.SetField(p, "waitLoad", noopZero)
			L.SetField(p, "html", noopEmptyStr)
			L.SetField(p, "cookies", noopEmptyTbl)
			L.SetField(p, "close", noopZero)
			L.Push(p)
			return 1
		})
		browserFn := L.NewFunction(func(L *lua.LState) int {
			b := L.NewTable()
			L.SetField(b, "page", pageFn)
			L.SetField(b, "close", noopZero)
			L.SetField(b, "sync_cookies", noopZero)
			L.Push(b)
			return 1
		})
		mod := L.NewTable()
		L.SetField(mod, "browser", browserFn)
		L.Push(mod)
		return 1
	})
}
