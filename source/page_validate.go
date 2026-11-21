package source

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ValidateImageBytes verifies that payload is a non-empty supported image (JPEG, PNG, GIF, WebP, AVIF)
// and not an HTML/JSON error or challenge page disguised with HTTP 200.
// It returns the canonical file extension (e.g. ".jpg", ".png", ".gif", ".webp", ".avif").
func ValidateImageBytes(payload []byte, contentType string) (string, error) {
	if len(payload) == 0 {
		return "", errors.New("empty image payload")
	}

	ct := strings.ToLower(strings.TrimSpace(contentType))
	if strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xhtml") || strings.Contains(ct, "application/json") {
		return "", fmt.Errorf("non-image content-type %q", contentType)
	}

	trimmed := bytes.TrimLeft(payload, " \t\r\n\xef\xbb\xbf")
	if len(trimmed) > 0 && (trimmed[0] == '<' || trimmed[0] == '{') {
		return "", errors.New("payload looks like HTML or JSON instead of an image")
	}

	// JPEG: FF D8 FF
	if len(payload) >= 3 && payload[0] == 0xFF && payload[1] == 0xD8 && payload[2] == 0xFF {
		return ".jpg", nil
	}

	// PNG: 89 50 4E 47 0D 0A 1A 0A
	if len(payload) >= 8 && bytes.Equal(payload[:8], []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) {
		return ".png", nil
	}

	// GIF: GIF87a or GIF89a
	if len(payload) >= 6 && (bytes.Equal(payload[:6], []byte("GIF87a")) || bytes.Equal(payload[:6], []byte("GIF89a"))) {
		return ".gif", nil
	}

	// WebP: RIFF at 0..3 and WEBP at 8..11
	if len(payload) >= 12 && bytes.Equal(payload[0:4], []byte("RIFF")) && bytes.Equal(payload[8:12], []byte("WEBP")) {
		return ".webp", nil
	}

	// AVIF: ISO BMFF ftyp box at offset 4..7 with avif/avis in major or compatible brands
	if isAVIF(payload) {
		return ".avif", nil
	}

	return "", errors.New("unsupported or unrecognized image magic bytes")
}

func isAVIF(payload []byte) bool {
	if len(payload) < 12 || !bytes.Equal(payload[4:8], []byte("ftyp")) {
		return false
	}

	boxSize := int(binary.BigEndian.Uint32(payload[0:4]))
	if boxSize < 12 {
		boxSize = len(payload)
	}
	if boxSize > len(payload) {
		boxSize = len(payload)
	}
	// Major brand at bytes 8..11
	major := payload[8:12]
	if bytes.Equal(major, []byte("avif")) || bytes.Equal(major, []byte("avis")) {
		return true
	}
	// Compatible brands start at byte 16 in 4-byte chunks within the ftyp box
	for off := 16; off+4 <= boxSize; off += 4 {
		brand := payload[off : off+4]
		if bytes.Equal(brand, []byte("avif")) || bytes.Equal(brand, []byte("avis")) {
			return true
		}
	}
	return false
}

// isCloudflareLikePayload checks whether an HTTP response (even with status 200)
// carries Cloudflare challenge indicators so errors include the "cloudflare" keyword.
func isCloudflareLikePayload(headers http.Header, body []byte) bool {
	if strings.EqualFold(headers.Get("cf-mitigated"), "challenge") {
		return true
	}
	if strings.Contains(strings.ToLower(headers.Get("Server")), "cloudflare") {
		return true
	}
	sample := body
	if len(sample) > 4096 {
		sample = sample[:4096]
	}
	lower := strings.ToLower(string(sample))
	for _, marker := range []string{"just a moment", "cf_chl_opt", "challenges.cloudflare.com", "_cf_chl_tk", "cloudflare"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func normalizePageExtension(current, detected string) string {
	cur := strings.ToLower(strings.TrimSpace(current))
	if detected == ".jpg" && cur == ".jpeg" {
		return ".jpeg"
	}
	return detected
}
