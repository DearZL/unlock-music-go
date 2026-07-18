package decrypt

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
)

const (
	musicExFooterSize  = 16
	musicExPayloadSize = 0xB0
)

var musicExMagic = []byte("musicex\x00")

// QQMusicExInfo describes the footer and audio range of a current QQ Music
// desktop download.
type QQMusicExInfo struct {
	DataLength   int
	FooterLength int
	InnerName    string
}

// IsQQMusicEx reports whether data carries a complete, supported musicex
// container. Legacy QMC files therefore continue through their own decoder.
func IsQQMusicEx(data []byte) bool {
	_, err := ParseQQMusicEx(data)
	return err == nil
}

// HasQQMusicExFooter identifies the musicex container family from its footer
// magic. ParseQQMusicEx additionally enforces the supported format version.
func HasQQMusicExFooter(data []byte) bool {
	return len(data) >= musicExFooterSize && bytes.Equal(data[len(data)-8:], musicExMagic)
}

// ParseQQMusicEx reads container metadata without touching the encrypted
// audio payload.
func ParseQQMusicEx(data []byte) (*QQMusicExInfo, error) {
	if len(data) < musicExFooterSize+musicExPayloadSize {
		return nil, errors.New("qqmusic/musicex: file too short")
	}
	footer := data[len(data)-musicExFooterSize:]
	if !HasQQMusicExFooter(data) {
		return nil, errors.New("qqmusic/musicex: footer magic not found")
	}

	footerLen := int(binary.LittleEndian.Uint32(footer[0:4]))
	version := binary.LittleEndian.Uint32(footer[4:8])
	if version != 1 || footerLen < musicExPayloadSize || footerLen > len(data) {
		return nil, fmt.Errorf("qqmusic/musicex: invalid footer (length=%d, version=%d)", footerLen, version)
	}

	dataLength := len(data) - footerLen
	payload := data[dataLength : dataLength+musicExPayloadSize]
	name, err := parseMusicExInnerName(payload)
	if err != nil {
		return nil, err
	}

	return &QQMusicExInfo{
		DataLength:   dataLength,
		FooterLength: footerLen,
		InnerName:    name,
	}, nil
}

func parseMusicExInnerName(payload []byte) (string, error) {
	if len(payload) < musicExPayloadSize {
		return "", errors.New("qqmusic/musicex: metadata payload too short")
	}

	// The name field starts at 0x48 and is zero-terminated UTF-16LE.
	var units []uint16
	for off := 0x48; off+1 < musicExPayloadSize; off += 2 {
		u := binary.LittleEndian.Uint16(payload[off : off+2])
		if u == 0 {
			break
		}
		units = append(units, u)
	}
	name := string(utf16.Decode(units))
	if strings.TrimSpace(name) == "" {
		return "", errors.New("qqmusic/musicex: internal file name is empty")
	}
	return name, nil
}
