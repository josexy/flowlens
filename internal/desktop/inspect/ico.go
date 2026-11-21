package inspect

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/sergeymakinen/go-ico" // Registers ICO for ui.DecodeBitmap.
)

// validateICO bounds the directory and the selected image before the codec
// allocates its payloads and pixels. ICO dimension bytes encode 256 as zero.
func validateICO(data []byte) error {
	const headerSize, entrySize, maxDimension = 6, 16, 256
	if len(data) < headerSize {
		return errors.New("ICO header is truncated")
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if count == 0 || count > (len(data)-headerSize)/entrySize {
		return errors.New("ICO directory is empty or truncated")
	}
	directoryEnd := headerSize + count*entrySize
	for offset := headerSize; offset < directoryEnd; offset += entrySize {
		entry := data[offset : offset+entrySize]
		size := uint64(binary.LittleEndian.Uint32(entry[8:12]))
		start := uint64(binary.LittleEndian.Uint32(entry[12:16]))
		if size == 0 || start < uint64(directoryEnd) || start > uint64(len(data)) || size > uint64(len(data))-start {
			return errors.New("ICO image is outside the body")
		}
	}
	config, err := ico.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return err
	}
	if config.Width < 1 || config.Height < 1 || config.Width > maxDimension || config.Height > maxDimension {
		return fmt.Errorf("ICO image dimensions %dx%d are outside 1..256", config.Width, config.Height)
	}
	return nil
}
