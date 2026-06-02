package decrypt

// xm.go — Xiami Music (.xm) decryption
//
// File layout:
//   [0x00] 4 bytes  – magic "ifmt"
//   [0x04] 4 bytes  – type marker: " WAV", "FLAC", " MP3", " A4M"
//   [0x08] 4 bytes  – magic 0xFEFEFEFE
//   [0x0C] 3 bytes  – data offset (little-endian uint24)
//   [0x0F] 1 byte   – encryption key
//   [0x10]          – audio data (starting at index `dataOffset` relative to [0x10])
//
// For each encrypted byte at position cur (>= dataOffset):
//   plainByte = (cipherByte - key) ^ 0xFF

import (
	"bytes"
	"errors"
)

var (
	xmMagic  = []byte{0x69, 0x66, 0x6D, 0x74} // "ifmt"
	xmMagic2 = []byte{0xFE, 0xFE, 0xFE, 0xFE}
)

var xmTypeMap = map[string]string{
	" WAV": "wav",
	"FLAC": "flac",
	" MP3": "mp3",
	" A4M": "m4a",
}

// XmResult holds the decrypted audio bytes.
type XmResult struct {
	Audio []byte
	Ext   string
	Mime  string
}

// DecryptXm decrypts a Xiami .xm file.
func DecryptXm(data []byte) (*XmResult, error) {
	if len(data) < 0x10 {
		return nil, errors.New("xm: file too short")
	}
	if !bytes.HasPrefix(data, xmMagic) {
		return nil, errors.New("xm: invalid magic header (expected 'ifmt')")
	}
	if !bytes.Equal(data[8:12], xmMagic2) {
		return nil, errors.New("xm: invalid secondary magic (expected 0xFEFEFEFE)")
	}

	typeText := string(data[4:8])
	ext, ok := xmTypeMap[typeText]
	if !ok {
		return nil, errors.New("xm: unknown file type: " + typeText)
	}

	// data offset is a 3-byte little-endian value at [0x0C..0x0F)
	dataOffset := int(data[0x0C]) | (int(data[0x0D]) << 8) | (int(data[0x0E]) << 16)
	key := data[0x0F]

	audio := make([]byte, len(data)-0x10)
	copy(audio, data[0x10:])
	if dataOffset > len(audio) {
		return nil, errors.New("xm: data offset exceeds audio payload")
	}

	for cur := dataOffset; cur < len(audio); cur++ {
		audio[cur] = (audio[cur] - key) ^ 0xFF
	}

	return &XmResult{
		Audio: audio,
		Ext:   ext,
		Mime:  AudioMimeType(ext),
	}, nil
}
