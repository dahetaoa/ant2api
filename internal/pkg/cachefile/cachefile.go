package cachefile

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

const (
	indexMagic   uint32 = 0x49445801
	indexVersion uint16 = 1

	indexHeaderSize = 10
	indexEntrySize  = 144

	recordIDSize = 128
)

var (
	ErrNotFound      = errors.New("cache record not found")
	ErrCorruptIndex  = errors.New("cache index corrupt")
	ErrCorruptRecord = errors.New("cache record corrupt")
)

type CacheWriter interface {
	Write(recordID string, data []byte) error
	Flush() error
	Close() error
}

type CacheReader interface {
	Load(recordID string) ([]byte, error)
	Exists(recordID string) bool
	Close() error
}

type CacheManager interface {
	GetReader(date string) (CacheReader, error)
	DeleteDate(date string) error
	Close() error
}

type IndexEntry struct {
	Offset uint64
	Length uint32
}

func validateDate(date string) error {
	if date == "" {
		return errors.New("date is empty")
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return fmt.Errorf("invalid date %q: %w", date, err)
	}
	return nil
}

func paths(baseDir string, date string) (datPath string, idxPath string) {
	name := date
	return filepath.Join(baseDir, name+".dat"), filepath.Join(baseDir, name+".idx")
}
