package http

import (
	"encoding/json"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	lua_json "github.com/kaizen-architecture/kaizen-mangal/internal/lualibs/json"
	"github.com/kaizen-architecture/kaizen-mangal/network"
	lua "github.com/yuin/gopher-lua"
)

const (
	// default http timeout
	DefaultTimeout = 2 * time.Minute
	// default don't ignore ssl
	insecureSkipVerify = false
)

// DefaultUserAgent returns the active browser profile's User-Agent.
func DefaultUserAgent() string {
	return network.ActiveProfile().UserAgent
}

type LuaClient struct {
	httpClient      network.HTTPClient
	userAgent       string
	basicAuthUser   *string
	basicAuthPasswd *string
	headers         map[string]string
	debug           bool
}

// newLuaClient() returns new LuaClient
func newLuaClient() *LuaClient {
	jar, _ := cookiejar.New(&cookiejar.Options{})
	hc, _ := network.NewClient(network.ClientOptions{
		Timeout: DefaultTimeout,
		Jar:     jar,
	})
	return &LuaClient{
		httpClient: hc,
		userAgent:  DefaultUserAgent(),
	}
}

func (client *LuaClient) updateRequest(req *http.Request) {
	// set basic auth
	if client.basicAuthUser != nil && client.basicAuthPasswd != nil {
		username, password := client.basicAuthUser, client.basicAuthPasswd
		req.SetBasicAuth(*username, *password)
	}
	// set user agent
	if client.userAgent != "" {
		req.Header.Set("User-Agent", client.userAgent)
	}
	// set headers
	if client.headers != nil {
		for k, v := range client.headers {
			req.Header.Set(k, v)
		}
	}
}

// DoRequest() process request with needed settings for request
func (client *LuaClient) DoRequest(req *http.Request) (*http.Response, error) {
	client.updateRequest(req)
	if client.debug {
		dump, _ := httputil.DumpRequestOut(req, true)
		log.Printf("[DEBUG] send request:\n%s\n", dump)
	}
	return client.httpClient.Do(req)
}

// PostFormRequest() process Form
func (client *LuaClient) PostFormRequest(targetURL string, data url.Values) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, targetURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return client.DoRequest(req)
}

func checkClient(L *lua.LState) *LuaClient {
	ud := L.CheckUserData(1)
	if v, ok := ud.Value.(*LuaClient); ok {
		return v
	}
	L.ArgError(1, "http client expected")
	return nil
}

// http.client(config) returns (user data, error)
// config table:
//
//	{
//	  proxy="http(s)://<user>:<password>@host:<port>",
//	  timeout= 10,
//	  insecure_ssl=false,
//	  user_agent = "gopher-lua",
//	  basic_auth_user = "",
//	  basic_auth_password = "",
//	  headers = {"key"="value"},
//	  debug = false,
//	}
func New(L *lua.LState) int {
	var config *lua.LTable
	if L.GetTop() > 0 {
		config = L.CheckTable(1)
	}

	opts := network.ClientOptions{
		Timeout:            DefaultTimeout,
		InsecureSkipVerify: insecureSkipVerify,
	}
	if proxyEnv := os.Getenv("HTTP_PROXY"); proxyEnv != "" {
		if _, err := url.Parse(proxyEnv); err == nil {
			opts.ProxyURL = proxyEnv
		}
	}

	client := &LuaClient{
		userAgent: DefaultUserAgent(),
	}

	if config != nil {
		config.ForEach(func(k lua.LValue, v lua.LValue) {
			switch k.String() {
			case "timeout":
				if value, ok := v.(lua.LNumber); ok {
					opts.Timeout = time.Duration(value) * time.Second
				} else {
					L.ArgError(1, "timeout must be number")
				}
			case "proxy":
				if value, ok := v.(lua.LString); ok {
					if _, err := url.Parse(value.String()); err == nil {
						opts.ProxyURL = value.String()
					} else {
						L.ArgError(1, "http_proxy must be http(s)://<user>:<password>@host:<port>")
					}
				} else {
					L.ArgError(1, "http_proxy must be string")
				}
			case "insecure_ssl":
				if value, ok := v.(lua.LBool); ok {
					opts.InsecureSkipVerify = bool(value)
				} else {
					L.ArgError(1, "insecure_ssl must be bool")
				}
			case "user_agent":
				if _, ok := v.(lua.LString); ok {
					client.userAgent = v.String()
				} else {
					L.ArgError(1, "user_agent must be string")
				}
			case "basic_auth_user":
				if _, ok := v.(lua.LString); ok {
					user := v.String()
					client.basicAuthUser = &user
				} else {
					L.ArgError(1, "basic_auth_user must be string")
				}
			case "basic_auth_password":
				if _, ok := v.(lua.LString); ok {
					password := v.String()
					client.basicAuthPasswd = &password
				} else {
					L.ArgError(1, "basic_auth_password must be string")
				}
			case "debug":
				if value, ok := v.(lua.LBool); ok {
					client.debug = bool(value)
				} else {
					L.ArgError(1, "debug must be bool")
				}
			case "headers":
				if tbl, ok := v.(*lua.LTable); ok {
					headers := make(map[string]string)
					data, err := lua_json.ValueEncode(tbl)
					if err != nil {
						L.ArgError(1, "headers must be table of key-values string")
					}
					if err := json.Unmarshal(data, &headers); err != nil {
						L.ArgError(1, "headers must be table of key-values string")
					}
					client.headers = headers
				} else {
					L.ArgError(1, "headers must be table")
				}
			}
		})
	}

	jar, _ := cookiejar.New(&cookiejar.Options{})
	opts.Jar = jar

	hc, err := network.NewClient(opts)
	if err != nil {
		L.ArgError(1, err.Error())
		return 0
	}
	client.httpClient = hc

	ud := L.NewUserData()
	ud.Value = client
	L.SetMetatable(ud, L.GetTypeMetatable("http_client_ud"))
	L.Push(ud)
	return 1
}
