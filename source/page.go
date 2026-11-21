package source

import (
	"bytes"
	"errors"
	"fmt"
	_ "image/gif"
	"io"
	"net/http"
	"net/url"

	"github.com/kaizen-architecture/kaizen-mangal/log"
	"github.com/kaizen-architecture/kaizen-mangal/network"
	"github.com/kaizen-architecture/kaizen-mangal/util"
)

// Page represents a page in a chapter
type Page struct {
	// URL of the page. Used to download the page.
	URL string `json:"url" jsonschema:"description=URL of the page. Used to download the image."`
	// Index of the page in the chapter.
	Index uint16 `json:"index" jsonschema:"description=Index of the page in the chapter."`
	// Extension of the page image.
	Extension string `json:"extension" jsonschema:"description=Extension of the page image."`
	// Size of the page in bytes
	Size uint64 `json:"-"`
	// Contents of the page
	Contents *bytes.Buffer `json:"-"`
	// Chapter that the page belongs to.
	Chapter *Chapter `json:"-"`
}

func (p *Page) request() (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, p.URL, nil)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	if p.Chapter != nil && p.Chapter.URL != "" {
		req.Header.Set("Referer", p.Chapter.URL)
	}
	req.Header.Set("User-Agent", network.ActiveProfile().UserAgent)
	return req, nil
}

// Download Page contents and validate image integrity (RFC 0001 Pillar E.1).
func (p *Page) Download() error {
	if p.URL == "" {
		err := fmt.Errorf("page #%d has empty URL", p.Index)
		log.Error(err)
		return err
	}

	log.Tracef("Downloading page #%d (%s)", p.Index, p.URL)

	req, err := p.request()
	if err != nil {
		return err
	}

	resp, err := network.Client.Do(req)
	if err != nil {
		log.Error(err)
		return err
	}

	defer util.Ignore(resp.Body.Close)

	host := ""
	if parsed, parseErr := url.Parse(p.URL); parseErr == nil {
		host = parsed.Host
	}

	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("http error from %s: %s", host, resp.Status)
		log.Error(err)
		return err
	}

	if resp.ContentLength == 0 {
		err = fmt.Errorf("http error from %s: nothing was returned", host)
		log.Error(err)
		return err
	}

	var (
		buf           []byte
		contentLength int64
	)

	// if the content length is unknown
	if resp.ContentLength == -1 {
		buf, err = io.ReadAll(resp.Body)
		contentLength = int64(len(buf))
	} else {
		contentLength = resp.ContentLength
		buf = make([]byte, resp.ContentLength)
		_, err = io.ReadFull(resp.Body, buf)
	}

	if err != nil {
		return err
	}

	detectedExt, valErr := ValidateImageBytes(buf, resp.Header.Get("Content-Type"))
	if valErr != nil {
		if isCloudflareLikePayload(resp.Header, buf) {
			err = fmt.Errorf("invalid page #%d from %s (cloudflare challenge): %w", p.Index, host, valErr)
		} else {
			err = fmt.Errorf("invalid page #%d from %s: %w", p.Index, host, valErr)
		}
		log.Error(err)
		return err
	}

	p.Extension = normalizePageExtension(p.Extension, detectedExt)
	p.Contents = bytes.NewBuffer(buf)
	p.Size = uint64(util.Max(contentLength, 0))

	log.Tracef("Page #%d downloaded", p.Index)
	return nil
}

// Close closes the page contents.
func (p *Page) Close() error {
	return nil
}

// Read reads from the page contents.
func (p *Page) Read(b []byte) (int, error) {
	log.Tracef("Reading page contents #%d", p.Index)
	if p.Contents == nil {
		err := errors.New("page not downloaded")
		log.Error(err)
		return 0, err
	}

	return p.Contents.Read(b)
}

// Filename generates a filename for the page.
func (p *Page) Filename() (filename string) {
	filename = fmt.Sprintf("%d%s", p.Index, p.Extension)
	filename = util.PadZero(filename, 10)

	return
}

func (p *Page) Source() Source {
	return p.Chapter.Source()
}
