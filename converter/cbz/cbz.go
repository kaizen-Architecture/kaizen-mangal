package cbz

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/kaizen-architecture/kaizen-mangal/filesystem"
	"github.com/kaizen-architecture/kaizen-mangal/key"
	"github.com/kaizen-architecture/kaizen-mangal/source"
	"github.com/spf13/viper"
)

type CBZ struct{}

func New() *CBZ {
	return &CBZ{}
}

func (*CBZ) Save(chapter *source.Chapter) (string, error) {
	return save(chapter, false)
}

func (*CBZ) SaveTemp(chapter *source.Chapter) (string, error) {
	return save(chapter, true)
}

func save(chapter *source.Chapter, temp bool) (path string, err error) {
	path, err = chapter.Path(temp)
	if err != nil {
		return
	}

	err = SaveTo(chapter, path)
	if err != nil {
		return "", err
	}

	return path, nil
}

// SaveTo writes a CBZ archive atomically: write to <to>.kaizen-tmp, check all close/sync
// errors, verify the ZIP central directory & entry count, then rename into place (RFC 0001 Pillar E.2).
func SaveTo(chapter *source.Chapter, to string) error {
	if chapter == nil || len(chapter.Pages) == 0 {
		return errors.New("cannot write cbz: chapter has no pages")
	}

	for i, page := range chapter.Pages {
		if page == nil || page.Contents == nil || page.Contents.Len() == 0 {
			return fmt.Errorf("cannot write cbz: page #%d has no contents", i)
		}
	}

	tmpPath := to + ".kaizen-tmp"
	cleanupTmp := func() {
		_ = filesystem.Api().Remove(tmpPath)
	}

	cbzFile, err := filesystem.Api().Create(tmpPath)
	if err != nil {
		return err
	}

	zipWriter := zip.NewWriter(cbzFile)
	expectedEntries := len(chapter.Pages)

	for _, page := range chapter.Pages {
		if err = addToZip(zipWriter, page.Contents, page.Filename()); err != nil {
			_ = zipWriter.Close()
			_ = cbzFile.Close()
			cleanupTmp()
			return err
		}
	}

	if viper.GetBool(key.MetadataComicInfoXML) {
		comicInfo := chapter.ComicInfo()
		marshalled, marshalErr := xml.MarshalIndent(comicInfo, "", "  ")
		if marshalErr != nil {
			_ = zipWriter.Close()
			_ = cbzFile.Close()
			cleanupTmp()
			return marshalErr
		}
		if err = addToZip(zipWriter, bytes.NewBuffer(marshalled), "ComicInfo.xml"); err != nil {
			_ = zipWriter.Close()
			_ = cbzFile.Close()
			cleanupTmp()
			return err
		}
		expectedEntries++
	}

	if err = zipWriter.Close(); err != nil {
		_ = cbzFile.Close()
		cleanupTmp()
		return fmt.Errorf("failed to finalize cbz central directory: %w", err)
	}

	if err = cbzFile.Sync(); err != nil {
		_ = cbzFile.Close()
		cleanupTmp()
		return fmt.Errorf("failed to sync cbz temp file: %w", err)
	}

	if err = cbzFile.Close(); err != nil {
		cleanupTmp()
		return fmt.Errorf("failed to close cbz temp file: %w", err)
	}

	if err = verifyZip(tmpPath, expectedEntries); err != nil {
		cleanupTmp()
		return fmt.Errorf("cbz verification failed: %w", err)
	}

	if err = atomicRename(tmpPath, to); err != nil {
		cleanupTmp()
		return fmt.Errorf("failed to publish cbz archive: %w", err)
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
		Name:   name,
		Method: zip.Store,
	}

	headerWriter, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}

	_, err = io.Copy(headerWriter, file)
	return err
}
