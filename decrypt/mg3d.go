package decrypt

// mg3d.go — Migu Music (.mg3d) decryption
//
// The file is an encrypted WAV. The encryption key is an ASCII hex string stored
// in the file itself (at some multiple of 0x20 between 0x20 and 0x140*0x20).
//
// Key discovery algorithm:
//  1. Try each segment at offsets 0x20, 0x40, ..., 0x280 (step 0x20, up to 0x140*0x20 = 0x2800).
//  2. Each candidate is a 0x20-byte region; all bytes must be printable uppercase hex (0-9, A-F).
//  3. Subtract the candidate key from the first 0x100 bytes and validate as WAV:
//     - bytes[0:4]  == "RIFF"
//     - bytes[8:16] == "WAVEfmt "
//     - fmt chunk size (LE uint32 at bytes[16]) in {16, 18, 40}
//     - subsequent chunk names must be printable ASCII
//  4. Use the first valid candidate as the decryption key for the whole file.

import (
	"bytes"
	"encoding/binary"
	"errors"
)

const mg3dSegmentSize = 0x20
const mg3dKeySearchLimit = 0x140 * mg3dSegmentSize

// Mg3dResult holds the decrypted audio bytes.
type Mg3dResult struct {
	Audio []byte
}

// DecryptMg3d decrypts a Migu Music .mg3d file.
func DecryptMg3d(data []byte) (*Mg3dResult, error) {
	if len(data) < 0x100 {
		return nil, errors.New("mg3d: file too short")
	}

	header := data[:0x100]
	var decryptionKey []byte

	for offset := mg3dSegmentSize; offset < mg3dKeySearchLimit; offset += mg3dSegmentSize {
		if offset+mg3dSegmentSize > len(data) {
			break
		}
		candidate := data[offset : offset+mg3dSegmentSize]
		if !allUpperHex(candidate) {
			continue
		}

		tempHeader := make([]byte, 0x100)
		decryptSegment(tempHeader, header, candidate)

		if !validateWavHeader(tempHeader) {
			continue
		}

		decryptionKey = candidate
		break
	}

	if decryptionKey == nil {
		return nil, errors.New("mg3d: no valid decryption key found")
	}

	audio := make([]byte, len(data))
	decryptSegment(audio, data, decryptionKey)

	return &Mg3dResult{Audio: audio}, nil
}

// decryptSegment subtracts key bytes (cycling) from src and writes to dst.
func decryptSegment(dst, src, key []byte) {
	n := len(src)
	if len(dst) < n {
		n = len(dst)
	}
	keyLen := len(key)
	for i := 0; i < n; i++ {
		dst[i] = src[i] - key[i%keyLen]
	}
}

// allUpperHex checks that all bytes are ASCII uppercase hex digits (0-9 or A-F).
func allUpperHex(b []byte) bool {
	for _, c := range b {
		if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// validateWavHeader validates the decrypted header as a WAV file.
func validateWavHeader(h []byte) bool {
	if len(h) < 0x18 {
		return false
	}
	if !bytes.Equal(h[0:4], []byte("RIFF")) {
		return false
	}
	if !bytes.Equal(h[8:16], []byte("WAVEfmt ")) {
		return false
	}
	fmtChunkSize := binary.LittleEndian.Uint32(h[16:20])
	if fmtChunkSize != 16 && fmtChunkSize != 18 && fmtChunkSize != 40 {
		return false
	}
	// Validate first data chunk name
	firstChunkOffset := 0x14 + int(fmtChunkSize)
	if firstChunkOffset+8 > len(h) {
		return true // can't validate further, accept
	}
	for _, c := range h[firstChunkOffset : firstChunkOffset+4] {
		if c < 0x20 || c > 0x7E {
			return false
		}
	}
	// Optionally validate second chunk name
	firstChunkDataSize := binary.LittleEndian.Uint32(h[firstChunkOffset+4 : firstChunkOffset+8])
	secondChunkOffset := firstChunkOffset + 8 + int(firstChunkDataSize)
	if secondChunkOffset+4 <= len(h) {
		for _, c := range h[secondChunkOffset : secondChunkOffset+4] {
			if c < 0x20 || c > 0x7E {
				return false
			}
		}
	}
	return true
}
