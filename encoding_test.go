package main

import (
	"encoding/binary"
	"testing"
)

func TestLyricsToUTF8DecodesUTF16SurrogatePairs(t *testing.T) {
	data := []byte{0xff, 0xfe, 0x3d, 0xd8, 0x00, 0xde}
	got, err := lyricsToUTF8(data)
	if err != nil {
		t.Fatal(err)
	}
	if got != "😀" {
		t.Fatalf("decoded UTF-16 = %q, want emoji", got)
	}
}

func TestDecodeUTF16DropsOnlyIncompleteTrailingByte(t *testing.T) {
	data := []byte{0x3d, 0xd8, 0x00, 0xde, 0xff}
	if got := decodeUTF16(data, binary.LittleEndian); got != "😀" {
		t.Fatalf("decoded UTF-16 = %q, want emoji", got)
	}
}
