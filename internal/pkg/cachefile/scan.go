package cachefile

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

func ScanIndex(idxPath string, fn func(recordID string, entry IndexEntry) error) error {
	if fn == nil {
		return fmt.Errorf("%w: nil scan callback", ErrCorruptIndex)
	}

	f, err := os.Open(idxPath)
	if err != nil {
		return err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if fi.Size() < indexHeaderSize {
		return fmt.Errorf("%w: index file too small", ErrCorruptIndex)
	}

	header := make([]byte, indexHeaderSize)
	if _, err := io.ReadFull(f, header); err != nil {
		return err
	}
	magic := binary.BigEndian.Uint32(header[:4])
	if magic != indexMagic {
		return fmt.Errorf("%w: bad magic 0x%08x", ErrCorruptIndex, magic)
	}
	ver := binary.BigEndian.Uint16(header[4:6])
	if ver != indexVersion {
		return fmt.Errorf("%w: bad version %d", ErrCorruptIndex, ver)
	}

	entryCount := int((fi.Size() - indexHeaderSize) / indexEntrySize)
	entryBuf := make([]byte, indexEntrySize)
	for i := 0; i < entryCount; i++ {
		if _, err := io.ReadFull(f, entryBuf); err != nil {
			return err
		}

		idTrimmed := trimZeroRight(entryBuf[:recordIDSize])
		if len(idTrimmed) == 0 {
			continue
		}
		recordID := string(idTrimmed)

		offset := binary.BigEndian.Uint64(entryBuf[recordIDSize : recordIDSize+8])
		length := binary.BigEndian.Uint32(entryBuf[recordIDSize+8 : recordIDSize+12])
		if err := fn(recordID, IndexEntry{Offset: offset, Length: length}); err != nil {
			return err
		}
	}
	return nil
}
