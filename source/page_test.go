package source

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaizen-architecture/kaizen-mangal/constant"
	"github.com/kaizen-architecture/kaizen-mangal/filesystem"
	"github.com/kaizen-architecture/kaizen-mangal/key"
	"github.com/spf13/viper"
)

func makeAVIF(majorBrand string, compatBrands ...string) []byte {
	size := 16 + 4*len(compatBrands)
	buf := make([]byte, size)
	binary.BigEndian.PutUint32(buf[0:4], uint32(size))
	copy(buf[4:8], "ftyp")
	copy(buf[8:12], majorBrand)
	for i, cb := range compatBrands {
		copy(buf[16+4*i:20+4*i], cb)
	}
	return buf
}

func TestValidateImageBytes(t *testing.T) {
	pngBytes := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00}
	jpgBytes := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}
	gifBytes := []byte("GIF89a\x01\x00\x01\x00")
	webpBytes := []byte("RIFF\x24\x00\x00\x00WEBP\x56\x50\x38")
	avifMajor := makeAVIF("avif", "mif1")
	avifCompat := makeAVIF("mif1", "miaf", "avif")

	validCases := []struct {
		name    string
		payload []byte
		ct      string
		wantExt string
	}{
		{"png", pngBytes, "image/png", ".png"},
		{"jpg", jpgBytes, "image/jpeg", ".jpg"},
		{"gif", gifBytes, "image/gif", ".gif"},
		{"webp", webpBytes, "image/webp", ".webp"},
		{"avif-major", avifMajor, "image/avif", ".avif"},
		{"avif-compat", avifCompat, "application/octet-stream", ".avif"},
	}

	for _, tc := range validCases {
		t.Run(tc.name, func(t *testing.T) {
			ext, err := ValidateImageBytes(tc.payload, tc.ct)
			if err != nil {
				t.Fatalf("ValidateImageBytes(%s) unexpected error: %v", tc.name, err)
			}
			if ext != tc.wantExt {
				t.Fatalf("ValidateImageBytes(%s) ext = %q, want %q", tc.name, ext, tc.wantExt)
			}
		})
	}

	invalidCases := []struct {
		name    string
		payload []byte
		ct      string
	}{
		{"empty", nil, "image/jpeg"},
		{"html-200", []byte("<!DOCTYPE html><title>Just a moment...</title>"), "text/html"},
		{"html-disguised-as-jpeg", []byte("<html><body>Challenge</body></html>"), "image/jpeg"},
		{"json-error", []byte(`{"error":"forbidden"}`), "application/json"},
		{"json-disguised", []byte(`  {"status":"blocked"}`), "image/png"},
		{"random-garbage", []byte("not-an-image-at-all"), "image/jpeg"},
		{"mp4-ftyp-not-avif", makeAVIF("isom", "mp42"), "video/mp4"},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ValidateImageBytes(tc.payload, tc.ct); err == nil {
				t.Fatalf("ValidateImageBytes(%s) expected error, got nil", tc.name)
			}
		})
	}
}

func TestPageDownloadRejectsHTMLChallengeAndCorrectsExtension(t *testing.T) {
	webpBytes := []byte("RIFF\x24\x00\x00\x00WEBP\x56\x50\x38\x00")
	mux := http.NewServeMux()
	mux.HandleFunc("/challenge.jpg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Server", "cloudflare")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "<!DOCTYPE html><html><title>Just a moment...</title></html>")
	})
	mux.HandleFunc("/actual-webp.jpg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/webp")
		_, _ = w.Write(webpBytes)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	chapter := &Chapter{Name: "Ch1", URL: srv.URL + "/ch1"}

	// Challenge HTML served with 200 and .jpg extension must fail with "cloudflare"
	p1 := &Page{URL: srv.URL + "/challenge.jpg", Index: 1, Extension: ".jpg", Chapter: chapter}
	err := p1.Download()
	if err == nil {
		t.Fatal("expected error for HTML challenge served with 200, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "cloudflare") {
		t.Fatalf("expected error to mention cloudflare, got: %v", err)
	}

	// Wrong scraper-guessed extension (.jpg) corrected to .webp
	p2 := &Page{URL: srv.URL + "/actual-webp.jpg", Index: 2, Extension: ".jpg", Chapter: chapter}
	if err := p2.Download(); err != nil {
		t.Fatalf("unexpected error downloading valid webp: %v", err)
	}
	if p2.Extension != ".webp" {
		t.Fatalf("expected Extension auto-corrected to .webp, got %q", p2.Extension)
	}
}

func TestDownloadPagesIntegrityAndRaceFree(t *testing.T) {
	pngBytes := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	}))
	defer srv.Close()

	viper.Set(key.DownloaderAsync, true)

	// Zero pages -> error
	emptyCh := &Chapter{Name: "Empty", Pages: nil}
	if err := emptyCh.DownloadPages(false, nil); err == nil {
		t.Fatal("expected error for zero-page chapter")
	}

	// Nil page -> error without panic
	nilPageCh := &Chapter{Name: "NilPage", Pages: []*Page{nil}}
	if err := nilPageCh.DownloadPages(false, nil); err == nil {
		t.Fatal("expected error for nil page")
	}

	// Empty URL page -> error without panic
	emptyURLCh := &Chapter{Name: "EmptyURL", Pages: []*Page{{Index: 1, URL: ""}}}
	if err := emptyURLCh.DownloadPages(false, nil); err == nil {
		t.Fatal("expected error for empty URL page")
	}

	// Concurrent async download of 10 valid pages -> race-free
	ch := &Chapter{Name: "Async10", URL: srv.URL + "/ch"}
	for i := 0; i < 10; i++ {
		ch.Pages = append(ch.Pages, &Page{
			URL:       fmt.Sprintf("%s/page/%d.png", srv.URL, i),
			Index:     uint16(i),
			Extension: ".png",
			Chapter:   ch,
		})
	}
	if err := ch.DownloadPages(false, func(string) {}); err != nil {
		t.Fatalf("async DownloadPages failed: %v", err)
	}
}

func TestIsDownloadedDetectsCorruptCBZ(t *testing.T) {
	filesystem.SetOsFs()
	t.Cleanup(filesystem.SetMemMapFs)

	dir := t.TempDir()
	viper.Set(key.DownloaderPath, dir)
	viper.Set(key.DownloaderCreateMangaDir, false)
	viper.Set(key.DownloaderChapterNameTemplate, "[{padded-index}] {chapter}")
	viper.Set(key.FormatsUse, constant.FormatCBZ)

	manga := &Manga{Name: "TestManga"}
	ch := &Chapter{Name: "Ch1", Index: 1, Manga: manga}
	cbzPath := filepath.Join(dir, ch.Filename())

	// Write a truncated/corrupt file at final CBZ path
	if err := os.WriteFile(cbzPath, []byte("PK\x03\x04truncated-garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ch.IsDownloaded() {
		t.Fatal("IsDownloaded() returned true for corrupt CBZ, want false")
	}

	// Reset memoized option and write a valid CBZ
	ch2 := &Chapter{Name: "Ch1", Index: 1, Manga: manga}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("000001.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cbzPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if !ch2.IsDownloaded() {
		t.Fatal("IsDownloaded() returned false for valid CBZ, want true")
	}
}
