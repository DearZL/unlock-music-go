package decrypt

// kwm.go — Kuwo Music (.kwm) decryption
//
// File layout:
//   [0x00] 16 bytes – magic header "yeelion-kuwo-tme" or "yeelion-kuwo\x00\x00\x00\x00"
//   [0x18] 8 bytes  – file key (little-endian uint64)
//   [0x400]         – audio data, XOR'd with a 32-byte mask derived from the file key
//
// Mask derivation:
//   1. Read uint64 from offset 0x18 (little-endian), convert to decimal string.
//   2. Pad or trim to 32 characters.
//   3. XOR each byte with the corresponding byte of PreDefinedKey.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

var (
	kwmMagicHeader   = []byte("yeelion-kuwo-tme")
	kwmMagicHeader2  = []byte("yeelion-kuwo\x00\x00\x00\x00")
	kwmPreDefinedKey = []byte("MoOtOiTvINGwd2E6n0E1i7L5t2IoOoNk")
)

// KwmResult holds the decrypted audio bytes.
type KwmResult struct {
	Audio []byte
	Ext   string
	Mime  string
}

// DecryptKwm decrypts a Kuwo .kwm file.
func DecryptKwm(data []byte) (*KwmResult, error) {
	if len(data) < 0x400+1 {
		return nil, errors.New("kwm: file too short")
	}
	if !bytes.HasPrefix(data, kwmMagicHeader) && !bytes.HasPrefix(data, kwmMagicHeader2) {
		return nil, errors.New("kwm: invalid magic header")
	}

	fileKey := data[0x18:0x20]
	mask := kwmCreateMask(fileKey)

	audio := make([]byte, len(data)-0x400)
	copy(audio, data[0x400:])
	for i := range audio {
		audio[i] ^= mask[i%32]
	}

	ext := SniffAudioExt(audio)
	return &KwmResult{
		Audio: audio,
		Ext:   ext,
		Mime:  AudioMimeType(ext),
	}, nil
}

// kwmCreateMask builds the 32-byte XOR mask from an 8-byte file key.
func kwmCreateMask(keyBytes []byte) [32]byte {
	keyNum := binary.LittleEndian.Uint64(keyBytes)
	keyStr := fmt.Sprintf("%d", keyNum) // decimal string
	keyStrTrim := kwmTrimKey(keyStr)

	var mask [32]byte
	for i := 0; i < 32; i++ {
		mask[i] = kwmPreDefinedKey[i] ^ keyStrTrim[i]
	}
	return mask
}

// kwmTrimKey pads or trims the key string to exactly 32 characters.
func kwmTrimKey(s string) string {
	if len(s) > 32 {
		return s[:32]
	}
	for len(s) < 32 {
		s += s
	}
	return s[:32]
}
