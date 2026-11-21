package source

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/kaizen-architecture/kaizen-mangal/constant"
	"github.com/kaizen-architecture/kaizen-mangal/filesystem"
	"github.com/kaizen-architecture/kaizen-mangal/key"
	"github.com/kaizen-architecture/kaizen-mangal/style"
	"github.com/kaizen-architecture/kaizen-mangal/util"
	"github.com/samber/mo"
	"github.com/spf13/viper"
)

// Chapter is a struct that represents a chapter of a manga.
type Chapter struct {
	// Name of the chapter
	Name string `json:"name" jsonschema:"description=Name of the chapter"`
	// URL of the chapter
	URL string `json:"url" jsonschema:"description=URL of the chapter"`
	// Index of the chapter in the manga.
	Index uint16 `json:"index" jsonschema:"description=Index of the chapter in the manga"`
	// ID of the chapter in the source.
	ID string `json:"id" jsonschema:"description=ID of the chapter in the source"`
	// Volume which the chapter belongs to.
	Volume string `json:"volume" jsonschema:"description=Volume which the chapter belongs to"`
	// Manga that the chapter belongs to.
	Manga *Manga `json:"-"`
	// Pages of the chapter.
	Pages []*Page `json:"pages" jsonschema:"description=Pages of the chapter"`

	isDownloaded mo.Option[bool]
	size         uint64
}

func (c *Chapter) String() string {
	return c.Name
}

// DownloadPages downloads the Pages contents of the Chapter.
// Pages needs to be set before calling this function.
func (c *Chapter) DownloadPages(temp bool, progress func(string)) error {
	if len(c.Pages) == 0 {
		c.isDownloaded = mo.Some(false)
		return errors.New("chapter has no pages, aborting download")
	}

	for i, page := range c.Pages {
		if page == nil {
			c.isDownloaded = mo.Some(false)
			return fmt.Errorf("page #%d is nil, aborting download", i)
		}
		if page.URL == "" {
			c.isDownloaded = mo.Some(false)
			return fmt.Errorf("page #%d has empty URL, aborting download", page.Index)
		}
	}

	atomic.StoreUint64(&c.size, 0)
	if progress == nil {
		progress = func(string) {}
	}

	status := func() string {
		return fmt.Sprintf(
			"Downloading %s %s",
			util.Quantify(len(c.Pages), "page", "pages"),
			style.Faint(c.SizeHuman()),
		)
	}

	var (
		wg         sync.WaitGroup
		errOnce    sync.Once
		firstErr   error
		aborted    atomic.Bool
		progressMu sync.Mutex
	)

	reportProgress := func() {
		progressMu.Lock()
		progress(status())
		progressMu.Unlock()
	}

	reportProgress()
	async := viper.GetBool(key.DownloaderAsync)

	for _, page := range c.Pages {
		page := page
		wg.Add(1)

		d := func() {
			defer wg.Done()

			if aborted.Load() {
				return
			}

			if err := page.Download(); err != nil {
				errOnce.Do(func() {
					firstErr = err
				})
				aborted.Store(true)
				return
			}

			atomic.AddUint64(&c.size, page.Size)
			reportProgress()
		}

		if async {
			go d()
		} else {
			d()
		}
	}

	wg.Wait()

	if firstErr != nil {
		c.isDownloaded = mo.Some(false)
		return firstErr
	}

	c.isDownloaded = mo.Some(!temp)
	return nil
}

// formattedName of the chapter according to the template in the config.
func (c *Chapter) formattedName() (name string) {
	name = viper.GetString(key.DownloaderChapterNameTemplate)

	var sourceName string
	if c.Source() != nil {
		sourceName = c.Source().Name()
	}

	for variable, value := range map[string]string{
		"manga":          c.Manga.Name,
		"chapter":        c.Name,
		"index":          fmt.Sprintf("%d", c.Index),
		"padded-index":   fmt.Sprintf("%04d", c.Index),
		"chapters-count": fmt.Sprintf("%d", len(c.Manga.Chapters)),
		"volume":         c.Volume,
		"source":         sourceName,
	} {
		name = strings.ReplaceAll(name, fmt.Sprintf("{%s}", variable), value)
	}

	return
}

// SizeHuman is the same as Size but returns a human-readable string.
func (c *Chapter) SizeHuman() string {
	return humanize.Bytes(atomic.LoadUint64(&c.size))
}

func (c *Chapter) Filename() (filename string) {
	filename = util.SanitizeFilename(c.formattedName())

	// plain format assumes that chapter is a directory with images
	// rather than a single file. So no need to add extension to it
	if f := viper.GetString(key.FormatsUse); f != constant.FormatPlain {
		return filename + "." + f
	}

	return
}

func (c *Chapter) IsDownloaded() bool {
	if c.isDownloaded.IsPresent() {
		return c.isDownloaded.MustGet()
	}

	path, _ := c.path(c.Manga.peekPath(), false)
	exists, _ := filesystem.Api().Exists(path)
	if !exists {
		c.isDownloaded = mo.Some(false)
		return false
	}

	// Cheap EOCD / central-directory check for .cbz and .zip archives (RFC 0001 Q4 / Pillar E.5).
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".cbz" || ext == ".zip" {
		if !isValidZipArchive(path) {
			c.isDownloaded = mo.Some(false)
			return false
		}
	}

	c.isDownloaded = mo.Some(true)
	return true
}

func isValidZipArchive(path string) bool {
	f, err := filesystem.Api().Open(path)
	if err != nil {
		return false
	}
	defer util.Ignore(f.Close)

	info, err := f.Stat()
	if err != nil || info.IsDir() || info.Size() <= 0 {
		return false
	}

	zr, err := zip.NewReader(f, info.Size())
	if err != nil {
		return false
	}
	return len(zr.File) > 0
}

func (c *Chapter) path(relativeTo string, createVolumeDir bool) (path string, err error) {
	if createVolumeDir {
		path = filepath.Join(path, util.SanitizeFilename(c.Volume))
		err = filesystem.Api().MkdirAll(path, os.ModePerm)
		if err != nil {
			return
		}
	}

	path = filepath.Join(relativeTo, c.Filename())
	return
}

func (c *Chapter) Path(temp bool) (path string, err error) {
	var manga string
	manga, err = c.Manga.Path(temp)
	if err != nil {
		return
	}

	return c.path(manga, c.Volume != "" && viper.GetBool(key.DownloaderCreateVolumeDir))
}

func (c *Chapter) Source() Source {
	return c.Manga.Source
}

func (c *Chapter) ComicInfo() *ComicInfo {
	var (
		day, month, year int
	)

	if viper.GetBool(key.MetadataComicInfoXMLAddDate) {
		if viper.GetBool(key.MetadataComicInfoXMLAlternativeDate) {
			// get current date
			t := time.Now()
			day = t.Day()
			month = int(t.Month())
			year = t.Year()
		} else {
			day = c.Manga.Metadata.StartDate.Day
			month = c.Manga.Metadata.StartDate.Month
			year = c.Manga.Metadata.StartDate.Year
		}
	} // empty dates will be omitted

	return &ComicInfo{
		XmlnsXsd: "http://www.w3.org/2001/XMLSchema",
		XmlnsXsi: "http://www.w3.org/2001/XMLSchema-instance",

		Title:      c.Name,
		Series:     c.Manga.Name,
		Number:     int(c.Index),
		Web:        c.URL,
		Genre:      strings.Join(c.Manga.Metadata.Genres, ","),
		PageCount:  len(c.Pages),
		Summary:    c.Manga.Metadata.Summary,
		Count:      c.Manga.Metadata.Chapters,
		Characters: strings.Join(c.Manga.Metadata.Characters, ","),
		Year:       year,
		Month:      month,
		Day:        day,
		Writer:     strings.Join(c.Manga.Metadata.Staff.Story, ","),
		Penciller:  strings.Join(c.Manga.Metadata.Staff.Art, ","),
		Letterer:   strings.Join(c.Manga.Metadata.Staff.Lettering, ","),
		Translator: strings.Join(c.Manga.Metadata.Staff.Translation, ","),
		Tags:       strings.Join(c.Manga.Metadata.Tags, ","),
		Notes:      "Downloaded with kaizen-mangal. https://github.com/kaizen-Architecture/kaizen-mangal",
		Manga:      "YesAndRightToLeft",
	}
}
