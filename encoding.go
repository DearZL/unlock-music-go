package main

// encoding.go — Detect and convert lyrics file encoding to UTF-8.
//
// Handles the encodings commonly used by Chinese LRC files on Windows:
//   UTF-8    (with or without BOM)
//   UTF-16 LE / BE  (with BOM)
//   GBK / GB2312    (no BOM, non-UTF-8 bytes)

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// lyricsToUTF8 converts raw lyrics file bytes to a clean UTF-8 string.
// Detection order:
//  1. UTF-8 BOM  → strip BOM, return as UTF-8
//  2. UTF-16 LE BOM (FF FE) → decode UTF-16 LE
//  3. UTF-16 BE BOM (FE FF) → decode UTF-16 BE
//  4. Valid UTF-8 (no BOM)  → use as-is
//  5. Fallback              → try GBK / GB18030
func lyricsToUTF8(data []byte) (string, error) {
	// 1. UTF-8 BOM
	if bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
		return string(data[3:]), nil
	}

	// 2. UTF-16 LE (FF FE)
	if bytes.HasPrefix(data, []byte{0xFF, 0xFE}) {
		return decodeUTF16(data[2:], binary.LittleEndian), nil
	}

	// 3. UTF-16 BE (FE FF)
	if bytes.HasPrefix(data, []byte{0xFE, 0xFF}) {
		return decodeUTF16(data[2:], binary.BigEndian), nil
	}

	// 4. Valid UTF-8 (no BOM)
	if utf8.Valid(data) {
		return string(data), nil
	}

	// 5. GBK / GB18030 (common on Windows for Chinese text)
	decoded, _, err := transform.Bytes(simplifiedchinese.GB18030.NewDecoder(), data)
	if err != nil {
		return "", fmt.Errorf("cannot decode lyrics: not UTF-8 and GBK decoding failed: %v", err)
	}
	return string(decoded), nil
}

// decodeUTF16 converts a BOM-stripped UTF-16 byte slice to a UTF-8 string.
func decodeUTF16(data []byte, order binary.ByteOrder) string {
	if len(data)%2 != 0 {
		data = data[:len(data)-1] // drop incomplete final code unit
	}
	codeUnits := make([]uint16, 0, len(data)/2)
	for i := 0; i < len(data); i += 2 {
		codeUnits = append(codeUnits, order.Uint16(data[i:i+2]))
	}
	return string(utf16.Decode(codeUnits))
}
