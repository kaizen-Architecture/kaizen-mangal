package zip

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/kaizen-architecture/kaizen-mangal/filesystem"
	"github.com/kaizen-architecture/kaizen-mangal/source"
)

type ZIP struct{}

func New() *ZIP {
	return &ZIP{}
}

func (*ZIP) Save(chapter *source.Chapter) (string, error) {
	return save(chapter, false)
}

func (*ZIP) SaveTemp(chapter *source.Chapter) (string, error) {
	return save(chapter, true)
}

func save(chapter *source.Chapter, temp bool) (path string, err error) {
	path, err = chapter.Path(temp)
	if err != nil {
		return
	}

	if err = SaveTo(chapter, path); err != nil {
		return "", err
	}

	return path, nil
}

// SaveTo writes a ZIP archive atomically with full close/sync/central-directory verification.
func SaveTo(chapter *source.Chapter, to string) error {
	if chapter == nil || len(chapter.Pages) == 0 {
		return errors.New("cannot write zip: chapter has no pages")
	}

	for i, page := range chapter.Pages {
		if page == nil || page.Contents == nil || page.Contents.Len() == 0 {
			return fmt.Errorf("cannot write zip: page #%d has no contents", i)
		}
	}

	tmpPath := to + ".kaizen-tmp"
	cleanupTmp := func() {
		_ = filesystem.Api().Remove(tmpPath)
	}

	zipFile, err := filesystem.Api().Create(tmpPath)
	if err != nil {
		return err
	}

	zipWriter := zip.NewWriter(zipFile)
	for _, page := range chapter.Pages {
		if err = addToZip(zipWriter, page.Contents, page.Filename()); err != nil {
			_ = zipWriter.Close()
			_ = zipFile.Close()
			cleanupTmp()
			return err
		}
	}

	if err = zipWriter.Close(); err != nil {
		_ = zipFile.Close()
		cleanupTmp()
		return fmt.Errorf("failed to finalize zip central directory: %w", err)
	}

	if err = zipFile.Sync(); err != nil {
		_ = zipFile.Close()
		cleanupTmp()
		return fmt.Errorf("failed to sync zip temp file: %w", err)
	}

	if err = zipFile.Close(); err != nil {
		cleanupTmp()
		return fmt.Errorf("failed to close zip temp file: %w", err)
	}

	if err = verifyZip(tmpPath, len(chapter.Pages)); err != nil {
		cleanupTmp()
		return fmt.Errorf("zip verification failed: %w", err)
	}

	if err = atomicRename(tmpPath, to); err != nil {
		cleanupTmp()
		return fmt.Errorf("failed to publish zip archive: %w", err)
	}

	return nil
}

func verifyZip(path string, expectedEntries int) error {
	f, err := filesystem.Api().Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() <= 0 {
		return errors.New("archive is empty")
	}

	zr, err := zip.NewReader(f, info.Size())
	if err != nil {
		return err
	}
	if len(zr.File) < 1 || len(zr.File) != expectedEntries {
		return fmt.Errorf("expected %d entries, got %d", expectedEntries, len(zr.File))
	}
	return nil
}

func atomicRename(src, dst string) error {
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		err = filesystem.Api().Rename(src, dst)
		if err == nil {
			return nil
		}
		time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
	}
	return err
}

func addToZip(writer *zip.Writer, file io.Reader, name string) error {
	header := &zip.FileHeader{
		Name:     name,
		Method:   zip.Deflate,
		Modified: time.Now(),
	}

	headerWriter, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}

	_, err = io.Copy(headerWriter, file)
	return err
}
