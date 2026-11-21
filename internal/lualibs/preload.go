package libs

import (
	"github.com/kaizen-architecture/kaizen-mangal/internal/lualibs/base64"
	"github.com/kaizen-architecture/kaizen-mangal/internal/lualibs/crypto"
	"github.com/kaizen-architecture/kaizen-mangal/internal/lualibs/filepath"
	"github.com/kaizen-architecture/kaizen-mangal/internal/lualibs/goos"
	"github.com/kaizen-architecture/kaizen-mangal/internal/lualibs/headless"
	"github.com/kaizen-architecture/kaizen-mangal/internal/lualibs/html"
	"github.com/kaizen-architecture/kaizen-mangal/internal/lualibs/http"
	"github.com/kaizen-architecture/kaizen-mangal/internal/lualibs/humanize"
	"github.com/kaizen-architecture/kaizen-mangal/internal/lualibs/inspect"
	"github.com/kaizen-architecture/kaizen-mangal/internal/lualibs/ioutil"
	"github.com/kaizen-architecture/kaizen-mangal/internal/lualibs/json"
	"github.com/kaizen-architecture/kaizen-mangal/internal/lualibs/log"
	"github.com/kaizen-architecture/kaizen-mangal/internal/lualibs/regexp"
	"github.com/kaizen-architecture/kaizen-mangal/internal/lualibs/runtime"
	"github.com/kaizen-architecture/kaizen-mangal/internal/lualibs/shellescape"
	"github.com/kaizen-architecture/kaizen-mangal/internal/lualibs/stats"
	"github.com/kaizen-architecture/kaizen-mangal/internal/lualibs/storage"
	"github.com/kaizen-architecture/kaizen-mangal/internal/lualibs/strings"
	"github.com/kaizen-architecture/kaizen-mangal/internal/lualibs/template"
	"github.com/kaizen-architecture/kaizen-mangal/internal/lualibs/time"
	"github.com/kaizen-architecture/kaizen-mangal/internal/lualibs/xmlpath"
	"github.com/kaizen-architecture/kaizen-mangal/internal/lualibs/yaml"
	lua "github.com/yuin/gopher-lua"
)

// Preload preload all gopher lua packages
func Preload(L *lua.LState) {
	for _, preload := range []func(*lua.LState){
		yaml.Preload,
		html.Preload,
		headless.Preload,
		xmlpath.Preload,
		time.Preload,
		template.Preload,
		strings.Preload,
		storage.Preload,
		stats.Preload,
		shellescape.Preload,
		runtime.Preload,
		regexp.Preload,
		log.Preload,
		json.Preload,
		ioutil.Preload,
		inspect.Preload,
		humanize.Preload,
		http.Preload,
		goos.Preload,
		filepath.Preload,
		crypto.Preload,
		base64.Preload,
	} {
		preload(L)
	}
}
