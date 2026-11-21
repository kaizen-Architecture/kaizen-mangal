package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kaizen-architecture/kaizen-mangal/constant"
	"github.com/kaizen-architecture/kaizen-mangal/internal/lualibs/headless"
	"github.com/kaizen-architecture/kaizen-mangal/key"
	"github.com/kaizen-architecture/kaizen-mangal/network"
	"github.com/kaizen-architecture/kaizen-mangal/where"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func init() {
	rootCmd.AddCommand(doctorCmd)
	doctorCmd.Flags().BoolP("json", "j", false, "output diagnostics as JSON")
	doctorCmd.SetOut(os.Stdout)
}

type doctorTLSProfile struct {
	Configured  string `json:"configured"`
	Resolved    string `json:"resolved"`
	ChromeMajor int    `json:"chrome_major"`
	Error       string `json:"error,omitempty"`
}

type doctorBrowser struct {
	Available     bool   `json:"available"`
	Mode          string `json:"mode"`
	Target        string `json:"target,omitempty"`
	AllowDownload bool   `json:"allow_download"`
	Error         string `json:"error,omitempty"`
}

type doctorSolver struct {
	Configured      bool   `json:"configured"`
	URL             string `json:"url,omitempty"`
	Reachable       bool   `json:"reachable"`
	ChromeMajor     int    `json:"chrome_major,omitempty"`
	FallbackEnabled bool   `json:"fallback_enabled"`
	Error           string `json:"error,omitempty"`
}

type doctorStorage struct {
	ConfigDir         string `json:"config_dir"`
	CacheDir          string `json:"cache_dir"`
	CacheWritable     bool   `json:"cache_writable"`
	CookieJarPath     string `json:"cookie_jar_path"`
	CookieJarWritable bool   `json:"cookie_jar_writable"`
}

type doctorReport struct {
	App             string           `json:"app"`
	Version         string           `json:"version"`
	ContractVersion int              `json:"contract_version"`
	OK              bool             `json:"ok"`
	TLSProfile      doctorTLSProfile `json:"tls_profile"`
	Browser         doctorBrowser    `json:"browser"`
	Solver          doctorSolver     `json:"solver"`
	Storage         doctorStorage    `json:"storage"`
	Warnings        []string         `json:"warnings"`
}

var chromeMajorRe = regexp.MustCompile(`(?i)Chrome/(\d+)`)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run diagnostics on TLS profile, headless browser, solver, and storage",
	Run: func(cmd *cobra.Command, args []string) {
		report := runDoctor()
		if lo.Must(cmd.Flags().GetBool("json")) {
			b, err := json.Marshal(report)
			handleErr(err)
			cmd.Println(string(b))
			return
		}

		cmd.Printf("%s %s (contract v%d)\n", report.App, report.Version, report.ContractVersion)
		cmd.Printf("  TLS profile : %s (configured=%q, chrome_major=%d)\n", report.TLSProfile.Resolved, report.TLSProfile.Configured, report.TLSProfile.ChromeMajor)
		cmd.Printf("  Browser     : mode=%s available=%v target=%q\n", report.Browser.Mode, report.Browser.Available, report.Browser.Target)
		if report.Browser.Error != "" {
			cmd.Printf("                error: %s\n", report.Browser.Error)
		}
		cmd.Printf("  Solver      : configured=%v reachable=%v fallback=%v\n", report.Solver.Configured, report.Solver.Reachable, report.Solver.FallbackEnabled)
		if report.Solver.Error != "" {
			cmd.Printf("                error: %s\n", report.Solver.Error)
		}
		cmd.Printf("  Cache dir   : %s (writable=%v)\n", report.Storage.CacheDir, report.Storage.CacheWritable)
		cmd.Printf("  Cookie jar  : %s (writable=%v)\n", report.Storage.CookieJarPath, report.Storage.CookieJarWritable)
		if len(report.Warnings) > 0 {
			cmd.Println("  Warnings:")
			for _, w := range report.Warnings {
				cmd.Printf("    - %s\n", w)
			}
		}
	},
}

func runDoctor() doctorReport {
	warnings := []string{}

	// 1. TLS Profile
	configuredProfile := strings.TrimSpace(viper.GetString(key.NetworkImpersonate))
	resolvedProfile, profErr := network.ResolveProfile(configuredProfile)
	tlsInfo := doctorTLSProfile{
		Configured: configuredProfile,
	}
	if profErr != nil {
		tlsInfo.Error = profErr.Error()
		fallback := network.ActiveProfile()
		tlsInfo.Resolved = fallback.Name
		tlsInfo.ChromeMajor = fallback.ChromeMajor
		warnings = append(warnings, fmt.Sprintf("invalid network.impersonate %q (%v); falling back to %s", configuredProfile, profErr, fallback.Name))
	} else {
		tlsInfo.Resolved = resolvedProfile.Name
		tlsInfo.ChromeMajor = resolvedProfile.ChromeMajor
	}

	// 2. Headless browser resolution
	res := headless.ResolveBrowser()
	allowDownload := true
	if viper.IsSet(key.HeadlessAllowDownload) {
		allowDownload = viper.GetBool(key.HeadlessAllowDownload)
	}
	browserInfo := doctorBrowser{
		Available:     res.Mode != "unavailable",
		Mode:          res.Mode,
		Target:        res.Target,
		AllowDownload: allowDownload,
		Error:         res.Error,
	}

	// 3. Cloudflare solver reachability
	solverURL := strings.TrimSpace(viper.GetString(key.NetworkCloudflareSolverURL))
	fallbackEnabled := viper.GetBool(key.NetworkCloudflareSolverFallback)
	solverInfo := doctorSolver{
		Configured:      solverURL != "",
		URL:             solverURL,
		FallbackEnabled: fallbackEnabled,
	}
	if solverURL != "" {
		reachable, chromeMajor, err := probeSolver(solverURL)
		solverInfo.Reachable = reachable
		solverInfo.ChromeMajor = chromeMajor
		if err != nil {
			solverInfo.Error = err.Error()
		}
	}

	// Warning (a): TLS profile Chrome major vs external solver Chrome major mismatch
	if tlsInfo.ChromeMajor > 0 && solverInfo.ChromeMajor > 0 && tlsInfo.ChromeMajor != solverInfo.ChromeMajor {
		warnings = append(warnings, fmt.Sprintf(
			"TLS profile %s (Chrome %d) differs from external solver Chrome major (%d); cf_clearance cookies may be rejected if versions diverge",
			tlsInfo.Resolved, tlsInfo.ChromeMajor, solverInfo.ChromeMajor,
		))
	}

	// Warning (b): Tier 2 headless fallback enabled with no browser/CDP URL available
	if fallbackEnabled && !browserInfo.Available {
		warnings = append(warnings, "network.cloudflare_solver_fallback is enabled but no local browser or headless.cdp_url is available")
	}

	// 4. Storage writability
	cacheDir := where.Cache()
	cookieJarPath := strings.TrimSpace(viper.GetString(key.NetworkCookieJarPath))
	if cookieJarPath == "" {
		cookieJarPath = filepath.Join(cacheDir, "kaizen-mangal-cookies.json")
	}
	cacheWritable := isDirWritable(cacheDir)
	cookieJarWritable := isDirWritable(filepath.Dir(cookieJarPath))

	storageInfo := doctorStorage{
		ConfigDir:         where.Config(),
		CacheDir:          cacheDir,
		CacheWritable:     cacheWritable,
		CookieJarPath:     cookieJarPath,
		CookieJarWritable: cookieJarWritable,
	}

	ok := cacheWritable && cookieJarWritable && (!solverInfo.Configured || solverInfo.Reachable) && profErr == nil

	return doctorReport{
		App:             constant.Mangal,
		Version:         constant.Version,
		ContractVersion: network.ContractVersion,
		OK:              ok,
		TLSProfile:      tlsInfo,
		Browser:         browserInfo,
		Solver:          solverInfo,
		Storage:         storageInfo,
		Warnings:        warnings,
	}
}

func probeSolver(rawURL string) (bool, int, error) {
	base := strings.TrimRight(rawURL, "/")
	probeURL := strings.TrimSuffix(base, "/v1")
	if probeURL == "" {
		probeURL = base
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return false, 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, 0, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
	chromeMajor := 0
	if m := chromeMajorRe.FindSubmatch(body); len(m) == 2 {
		if n, convErr := strconv.Atoi(string(m[1])); convErr == nil {
			chromeMajor = n
		}
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 500 {
		return true, chromeMajor, nil
	}
	return false, chromeMajor, fmt.Errorf("solver returned HTTP %d", resp.StatusCode)
}

func isDirWritable(dir string) bool {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	f, err := os.CreateTemp(dir, ".kaizen-mangal-doctor-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}
