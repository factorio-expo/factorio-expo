package artifacts

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"os"
)

// validatePNGChunks walks every chunk of a PNG of the given size: signature,
// IHDR first, CRCs, at least one IDAT, and an IEND that ends the file exactly.
func validatePNGChunks(input *os.File, size int64) error {
	const signature = "\x89PNG\r\n\x1a\n"
	header := make([]byte, len(signature))
	if _, err := io.ReadFull(input, header); err != nil || !bytes.Equal(header, []byte(signature)) {
		return errors.New("invalid PNG signature")
	}
	var seenIHDR, seenIDAT bool
	remaining := size - int64(len(signature))
	for remaining > 0 {
		if remaining < 12 {
			return errors.New("truncated PNG chunk")
		}
		var length uint32
		if err := binary.Read(input, binary.BigEndian, &length); err != nil {
			return err
		}
		var chunkType [4]byte
		if _, err := io.ReadFull(input, chunkType[:]); err != nil {
			return err
		}
		chunkSize := int64(length) + 12
		if chunkSize > remaining {
			return errors.New("PNG chunk exceeds file size")
		}
		if !seenIHDR && (string(chunkType[:]) != "IHDR" || length != 13) {
			return errors.New("PNG does not start with IHDR")
		}
		checksum := crc32.NewIEEE()
		_, _ = checksum.Write(chunkType[:])
		if _, err := io.CopyN(checksum, input, int64(length)); err != nil {
			return err
		}
		var expectedCRC uint32
		if err := binary.Read(input, binary.BigEndian, &expectedCRC); err != nil {
			return err
		}
		if checksum.Sum32() != expectedCRC {
			return errors.New("PNG chunk checksum mismatch")
		}
		switch string(chunkType[:]) {
		case "IHDR":
			if seenIHDR {
				return errors.New("PNG contains multiple IHDR chunks")
			}
			seenIHDR = true
		case "IDAT":
			seenIDAT = true
		case "IEND":
			if length != 0 || !seenIDAT || remaining != chunkSize {
				return errors.New("invalid PNG IEND chunk")
			}
			return nil
		}
		remaining -= chunkSize
	}
	return errors.New("PNG is missing IEND")
}
