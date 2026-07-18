package decrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseQQMusicEx(t *testing.T) {
	const innerName = "F0M00013B1pp3ekkhJ.mflac"
	data := make([]byte, musicExFooterSize+musicExPayloadSize)
	copy(data, []byte{0xAA, 0xBB, 0xCC})
	payload := data[musicExFooterSize:]
	for i, r := range []rune(innerName) {
		binary.LittleEndian.PutUint16(payload[0x48+i*2:], uint16(r))
	}
	footer := data[len(data)-musicExFooterSize:]
	binary.LittleEndian.PutUint32(footer[0:4], musicExPayloadSize)
	binary.LittleEndian.PutUint32(footer[4:8], 1)
	copy(footer[8:], musicExMagic)

	info, err := ParseQQMusicEx(data)
	if err != nil {
		t.Fatal(err)
	}
	if info.DataLength != musicExFooterSize || info.FooterLength != musicExPayloadSize || info.InnerName != innerName {
		t.Fatalf("unexpected parsed info: %+v", info)
	}
	if !IsQQMusicEx(data) {
		t.Fatal("IsQQMusicEx returned false for a valid container")
	}

	// The magic still marks a musicex-family file after a version bump, while
	// strict parsing rejects the unsupported version.  The dispatcher uses the
	// former check, preventing an accidental legacy-QMC fallback.
	binary.LittleEndian.PutUint32(footer[4:8], 2)
	if !HasQQMusicExFooter(data) {
		t.Fatal("HasQQMusicExFooter returned false for musicex v2")
	}
	if IsQQMusicEx(data) {
		t.Fatal("IsQQMusicEx accepted an unsupported musicex version")
	}
	if _, err := ParseQQMusicEx(data); err == nil {
		t.Fatal("ParseQQMusicEx accepted an unsupported musicex version")
	}
}

func TestFindQQMusicEKeyMatchesInternalName(t *testing.T) {
	const innerName = "F0M00013B1pp3ekkhJ.mflac"
	wrong := strings.Repeat("A", 120)
	want := strings.Repeat("B", 160)
	plain := []byte("old record\x00" + wrong + "\x00metadata\x00" + innerName + "\x00" + want)

	got, err := findQQMusicEKey(plain, innerName)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q, want matching ekey %q", got, want)
	}
}

func TestDecryptQQMusicMMKV(t *testing.T) {
	key := "0123456789abcdef-qqmusic"
	plain := []byte("record\x00F0M00013B1pp3ekkhJ.mflac\x00" + strings.Repeat("X", 100))
	block, err := aes.NewCipher([]byte(key[:aes.BlockSize]))
	if err != nil {
		t.Fatal(err)
	}
	encoded := make([]byte, 4+len(plain))
	binary.LittleEndian.PutUint32(encoded, uint32(len(plain)))
	cipher.NewCFBEncrypter(block, make([]byte, aes.BlockSize)).XORKeyStream(encoded[4:], plain)

	path := filepath.Join(t.TempDir(), "Checkccae.dat")
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := decryptQQMusicMMKV(path, key)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(plain) {
		t.Fatalf("MMKV plaintext mismatch: got %q, want %q", got, plain)
	}
}
