package cachefile

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

type Writer struct {
	date    string
	datPath string
	idxPath string

	mu     sync.RWMutex
	closed bool

	dataFile   *os.File
	dataFD     uintptr
	dataOffset uint64

	idxFile  *os.File
	idxCount uint32
	idx      map[string]IndexEntry
}

func NewCacheWriter(baseDir string, date string) (*Writer, error) {
	if err := validateDate(date); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, err
	}

	datPath, idxPath := paths(baseDir, date)

	dataFile, dataOffset, err := openDataFile(datPath)
	if err != nil {
		return nil, err
	}

	idxFile, idxCount, err := openIndexFile(idxPath)
	if err != nil {
		_ = dataFile.Close()
		return nil, err
	}

	idxMap, err := loadIndexFile(idxPath)
	if err != nil {
		_ = idxFile.Close()
		_ = dataFile.Close()
		return nil, err
	}

	return &Writer{
		date:       date,
		datPath:    datPath,
		idxPath:    idxPath,
		dataFile:   dataFile,
		dataFD:     dataFile.Fd(),
		dataOffset: dataOffset,
		idxFile:    idxFile,
		idxCount:   idxCount,
		idx:        idxMap,
	}, nil
}

func (w *Writer) Exists(recordID string) bool {
	if recordID == "" {
		return false
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		return false
	}
	_, ok := w.idx[recordID]
	return ok
}

func (w *Writer) Load(recordID string) ([]byte, error) {
	if recordID == "" {
		return nil, ErrNotFound
	}

	w.mu.RLock()
	if w.closed {
		w.mu.RUnlock()
		return nil, errors.New("cache writer closed")
	}
	entry, ok := w.idx[recordID]
	f := w.dataFile
	fd := w.dataFD
	w.mu.RUnlock()

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

func (w *Writer) Write(recordID string, data []byte) error {
	if recordID == "" {
		return errors.New("recordID is empty")
	}
	if len(recordID) > recordIDSize {
		return fmt.Errorf("recordID too long: %d > %d", len(recordID), recordIDSize)
	}
	if uint64(len(data)) > uint64(^uint32(0)) {
		return fmt.Errorf("record too large: %d > %d", len(data), uint64(^uint32(0)))
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errors.New("cache writer closed")
	}

	recordOffset := w.dataOffset
	length := uint32(len(data))

	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], length)

	if err := writeFull(w.dataFile, hdr[:]); err != nil {
		return err
	}
	if err := writeFull(w.dataFile, data); err != nil {
		_ = w.dataFile.Truncate(int64(recordOffset))
		w.dataOffset = recordOffset
		return err
	}

	w.dataOffset += uint64(4 + len(data))

	var entryBuf [indexEntrySize]byte
	copy(entryBuf[:recordIDSize], recordID)
	binary.BigEndian.PutUint64(entryBuf[recordIDSize:recordIDSize+8], recordOffset)
	binary.BigEndian.PutUint32(entryBuf[recordIDSize+8:recordIDSize+12], length)
	// entryBuf[recordIDSize+12:recordIDSize+16] reserved zeros

	if err := writeFull(w.idxFile, entryBuf[:]); err != nil {
		_ = w.dataFile.Truncate(int64(recordOffset))
		w.dataOffset = recordOffset
		return err
	}

	w.idxCount++
	if err := w.writeHeaderCount(); err != nil {
		return err
	}

	w.idx[recordID] = IndexEntry{Offset: recordOffset, Length: length}
	return nil
}

func (w *Writer) Flush() error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		return nil
	}
	if err := w.dataFile.Sync(); err != nil {
		return err
	}
	if err := w.idxFile.Sync(); err != nil {
		return err
	}
	return nil
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true

	var errs []error
	if err := w.writeHeaderCount(); err != nil {
		errs = append(errs, err)
	}
	if w.idxFile != nil {
		if err := w.idxFile.Close(); err != nil {
			errs = append(errs, err)
		}
		w.idxFile = nil
	}
	if w.dataFile != nil {
		if err := w.dataFile.Close(); err != nil {
			errs = append(errs, err)
		}
		w.dataFile = nil
	}
	w.idx = nil
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (w *Writer) writeHeaderCount() error {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], w.idxCount)
	_, err := w.idxFile.WriteAt(buf[:], 6)
	return err
}

func openDataFile(datPath string) (*os.File, uint64, error) {
	f, err := os.OpenFile(datPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, 0, err
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, 0, err
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		_ = f.Close()
		return nil, 0, err
	}
	return f, uint64(fi.Size()), nil
}

func openIndexFile(idxPath string) (*os.File, uint32, error) {
	f, err := os.OpenFile(idxPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, 0, err
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, 0, err
	}

	if fi.Size() == 0 {
		if err := writeIndexHeader(f, 0); err != nil {
			_ = f.Close()
			return nil, 0, err
		}
		if _, err := f.Seek(indexHeaderSize, io.SeekStart); err != nil {
			_ = f.Close()
			return nil, 0, err
		}
		return f, 0, nil
	}
	if fi.Size() < indexHeaderSize {
		_ = f.Close()
		return nil, 0, fmt.Errorf("%w: index file too small", ErrCorruptIndex)
	}

	header := make([]byte, indexHeaderSize)
	if _, err := io.ReadFull(f, header); err != nil {
		_ = f.Close()
		return nil, 0, err
	}
	magic := binary.BigEndian.Uint32(header[:4])
	if magic != indexMagic {
		_ = f.Close()
		return nil, 0, fmt.Errorf("%w: bad magic 0x%08x", ErrCorruptIndex, magic)
	}
	ver := binary.BigEndian.Uint16(header[4:6])
	if ver != indexVersion {
		_ = f.Close()
		return nil, 0, fmt.Errorf("%w: bad version %d", ErrCorruptIndex, ver)
	}

	// Trust file size over header count for resilience to crashes/partial writes.
	entryCount := uint32((fi.Size() - indexHeaderSize) / indexEntrySize)
	end := int64(indexHeaderSize) + int64(entryCount)*indexEntrySize
	if _, err := f.Seek(end, io.SeekStart); err != nil {
		_ = f.Close()
		return nil, 0, err
	}
	return f, entryCount, nil
}

func writeIndexHeader(f *os.File, count uint32) error {
	var hdr [indexHeaderSize]byte
	binary.BigEndian.PutUint32(hdr[0:4], indexMagic)
	binary.BigEndian.PutUint16(hdr[4:6], indexVersion)
	binary.BigEndian.PutUint32(hdr[6:10], count)
	_, err := f.Write(hdr[:])
	return err
}

func writeFull(f *os.File, buf []byte) error {
	for len(buf) > 0 {
		n, err := f.Write(buf)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		buf = buf[n:]
	}
	return nil
}
