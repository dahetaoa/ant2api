package cachefile

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

type Reader struct {
	date    string
	datPath string
	idxPath string

	mu     sync.RWMutex
	closed bool

	f   *os.File
	fd  uintptr
	idx map[string]IndexEntry
}

func NewCacheReader(baseDir string, date string) (*Reader, error) {
	if err := validateDate(date); err != nil {
		return nil, err
	}
	datPath, idxPath := paths(baseDir, date)

	idxMap, err := loadIndexFile(idxPath)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(datPath)
	if err != nil {
		return nil, err
	}

	return &Reader{
		date:    date,
		datPath: datPath,
		idxPath: idxPath,
		f:       f,
		fd:      f.Fd(),
		idx:     idxMap,
	}, nil
}

func (r *Reader) Exists(recordID string) bool {
	if recordID == "" {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return false
	}
	_, ok := r.idx[recordID]
	return ok
}

func (r *Reader) Load(recordID string) ([]byte, error) {
	if recordID == "" {
		return nil, ErrNotFound
	}

	r.mu.RLock()
	if r.closed {
		r.mu.RUnlock()
		return nil, errors.New("cache reader closed")
	}
	entry, ok := r.idx[recordID]
	f := r.f
	fd := r.fd
	r.mu.RUnlock()

	if !ok {
		return nil, ErrNotFound
	}

	total := int64(entry.Length) + 4
	if total < 4 {
		return nil, ErrCorruptRecord
	}

	buf := make([]byte, total)
	n, err := f.ReadAt(buf, int64(entry.Offset))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if int64(n) != total {
		return nil, ErrCorruptRecord
	}

	declared := binary.BigEndian.Uint32(buf[:4])
	if declared != entry.Length {
		return nil, ErrCorruptRecord
	}

	_ = fadviseDontNeed(fd, int64(entry.Offset), total)
	return buf[4:], nil
}

func (r *Reader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	r.idx = nil
	return err
}

func loadIndexFile(idxPath string) (map[string]IndexEntry, error) {
	f, err := os.Open(idxPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if fi.Size() < indexHeaderSize {
		return nil, fmt.Errorf("%w: index file too small", ErrCorruptIndex)
	}
	if (fi.Size()-indexHeaderSize)%indexEntrySize != 0 {
		// Ignore trailing partial writes by truncating count to whole entries.
	}

	header := make([]byte, indexHeaderSize)
	if _, err := io.ReadFull(f, header); err != nil {
		return nil, err
	}
	magic := binary.BigEndian.Uint32(header[:4])
	if magic != indexMagic {
		return nil, fmt.Errorf("%w: bad magic 0x%08x", ErrCorruptIndex, magic)
	}
	ver := binary.BigEndian.Uint16(header[4:6])
	if ver != indexVersion {
		return nil, fmt.Errorf("%w: bad version %d", ErrCorruptIndex, ver)
	}
	// count := binary.BigEndian.Uint32(header[6:10]) // optional; ignore and derive from file size.

	entryCount := int((fi.Size() - indexHeaderSize) / indexEntrySize)
	idx := make(map[string]IndexEntry, entryCount)

	entryBuf := make([]byte, indexEntrySize)
	for i := 0; i < entryCount; i++ {
		if _, err := io.ReadFull(f, entryBuf); err != nil {
			return nil, err
		}

		idBytes := entryBuf[:recordIDSize]
		idTrimmed := trimZeroRight(idBytes)
		if len(idTrimmed) == 0 {
			continue
		}
		recordID := string(idTrimmed)

		offset := binary.BigEndian.Uint64(entryBuf[recordIDSize : recordIDSize+8])
		length := binary.BigEndian.Uint32(entryBuf[recordIDSize+8 : recordIDSize+12])

		idx[recordID] = IndexEntry{Offset: offset, Length: length}
	}

	return idx, nil
}

func trimZeroRight(b []byte) []byte {
	i := len(b)
	for i > 0 && b[i-1] == 0x00 {
		i--
	}
	return b[:i]
}
