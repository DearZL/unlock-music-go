//go:build darwin

package decrypt

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestQQMusicMacGMCK(t *testing.T) {
	const openUDID = "01234abcdef0123456789abcdef0123456789abc"

	gotID, gotKey, err := qqMusicMacGMCK(openUDID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if want := "45678"; gotID != want {
		t.Fatalf("mmap ID = %q, want %q", gotID, want)
	}
	if want := "22a701a1bb91a02d8536955cdf06e1e2"; gotKey != want {
		t.Fatalf("encryption key = %q, want %q", gotKey, want)
	}
}

func TestResolveQQMusicEKeyMacOS(t *testing.T) {
	const (
		openUDID = "01234abcdef0123456789abcdef0123456789abc"
		mmapID   = "45678"
		keyHex   = "22a701a1bb91a02d8536955cdf06e1e2"
		filePath = "/tmp/QQMusicMac/iQmc/测试曲目.mflac"
	)

	tests := []struct {
		name        string
		decodedSize int
	}{
		{name: "364 character EKey", decodedSize: 273},
		{name: "704 character EKey", decodedSize: 528},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appSupportPath := filepath.Join(t.TempDir(), "Data", "Library", "Application Support", "QQMusicMac")
			preferencesPath := qqMusicMacPreferencesPath(appSupportPath)
			if err := os.MkdirAll(filepath.Dir(preferencesPath), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(appSupportPath, "iData"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(preferencesPath, qqMusicMacTestBinaryPlist(openUDID), 0600); err != nil {
				t.Fatal(err)
			}

			want := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x5a}, tt.decodedSize))
			stale := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x41}, tt.decodedSize))
			plain := qqMusicMacTestMMKVPlain(
				qqMusicMacTestMMKVRecord("/tmp/QQMusicMac/iQmc/another.mflac", stale),
				qqMusicMacTestMMKVRecord(filePath, stale),
				qqMusicMacTestMMKVRecord(filePath, want),
			)
			data, meta := qqMusicMacTestEncryptMMKV(t, plain, keyHex)
			dataPath := filepath.Join(appSupportPath, "iData", mmapID)
			if err := os.WriteFile(dataPath, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dataPath+".crc", meta, 0600); err != nil {
				t.Fatal(err)
			}

			got, err := resolveQQMusicEKey(QQMusicExInfo{InnerName: "different-internal-name.mflac"}, QQMusicOptions{
				MacAppSupportPath: appSupportPath,
				FilePath:          filePath,
			})
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("EKey mismatch: got %q, want %q", got, want)
			}
		})
	}
}

func TestResolveQQMusicEKeyMacOSRequiresAbsolutePath(t *testing.T) {
	_, err := resolveQQMusicEKey(QQMusicExInfo{}, QQMusicOptions{FilePath: "relative.mflac"})
	if err == nil {
		t.Fatal("resolveQQMusicEKey accepted a relative path")
	}
}

func TestQQMusicMacAppSupportPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := qqMusicMacAppSupportPaths("")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(home, "Library", "Containers", qqMusicMacBundleID, "Data", "Library", "Application Support", "QQMusicMac"),
		filepath.Join(home, "Library", "Application Support", "QQMusicMac"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default paths = %#v, want %#v", got, want)
	}

	override := filepath.Join(home, "custom", "QQMusicMac")
	got, err = qqMusicMacAppSupportPaths(override)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{override}) {
		t.Fatalf("override paths = %#v, want %#v", got, []string{override})
	}
}

func TestQQMusicMacPreferencesPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	appSupportPath := filepath.Join(home, "Library", "Application Support", "QQMusicMac")

	got := qqMusicMacPreferencesPaths(appSupportPath)
	want := []string{
		filepath.Join(home, "Library", "Preferences", qqMusicMacBundleID+".plist"),
		filepath.Join(home, "Library", "Containers", qqMusicMacBundleID, "Data", "Library", "Preferences", qqMusicMacBundleID+".plist"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("preferences paths = %#v, want %#v", got, want)
	}
}

func TestQQMusicMacOpenUDIDFromXMLPlist(t *testing.T) {
	const want = "01234abcdef0123456789abcdef0123456789abc"
	raw := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict><key>OpenUDID</key><dict><key>OpenUDID</key><string>` + want + `</string></dict></dict></plist>`)
	got, err := qqMusicMacOpenUDIDFromXMLPlist(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("OpenUDID = %q, want %q", got, want)
	}
}

func qqMusicMacTestMMKVPlain(records ...[]byte) []byte {
	// MiniPBCoder writes and ignores this root ItemSizeHolder varint.
	plain := []byte{0xff, 0xff, 0xff, 0x07}
	for _, record := range records {
		plain = append(plain, record...)
	}
	return plain
}

func qqMusicMacTestMMKVRecord(path, ekey string) []byte {
	storedValue := qqMusicMacTestVarint(uint64(len(ekey)))
	storedValue = append(storedValue, ekey...)

	record := qqMusicMacTestVarint(uint64(len(path)))
	record = append(record, path...)
	record = append(record, qqMusicMacTestVarint(uint64(len(storedValue)))...)
	record = append(record, storedValue...)
	return record
}

func qqMusicMacTestVarint(value uint64) []byte {
	var out []byte
	for value >= 0x80 {
		out = append(out, byte(value&0x7f)|0x80)
		value >>= 7
	}
	return append(out, byte(value))
}

func qqMusicMacTestEncryptMMKV(t *testing.T, plain []byte, keyHex string) ([]byte, []byte) {
	t.Helper()
	block, err := aes.NewCipher([]byte(keyHex[:aes.BlockSize]))
	if err != nil {
		t.Fatal(err)
	}

	meta := make([]byte, qqMusicMacMMKVMetaSize)
	binary.LittleEndian.PutUint32(meta[4:8], 3)
	for i := 0; i < aes.BlockSize; i++ {
		meta[qqMusicMacMMKVIVOffset+i] = byte(i + 1)
	}
	binary.LittleEndian.PutUint32(meta[qqMusicMacMMKVActualOff:qqMusicMacMMKVActualOff+4], uint32(len(plain)))

	data := make([]byte, 4+len(plain))
	binary.LittleEndian.PutUint32(data[:4], uint32(len(plain)))
	cipher.NewCFBEncrypter(block, meta[qqMusicMacMMKVIVOffset:qqMusicMacMMKVIVOffset+aes.BlockSize]).XORKeyStream(data[4:], plain)
	return data, meta
}

func qqMusicMacTestBinaryPlist(openUDID string) []byte {
	objects := [][]byte{
		{0xd1, 0x03, 0x01}, // root: "OpenUDID" -> object 1
		{0xd1, 0x03, 0x02}, // nested: "OpenUDID" -> object 2
		qqMusicMacTestASCIIPlistString(openUDID),
		qqMusicMacTestASCIIPlistString("OpenUDID"),
	}

	raw := []byte("bplist00")
	offsets := make([]int, len(objects))
	for i, object := range objects {
		offsets[i] = len(raw)
		raw = append(raw, object...)
	}
	offsetTableFrom := len(raw)
	for _, offset := range offsets {
		raw = append(raw, byte(offset))
	}

	trailer := make([]byte, 32)
	trailer[6] = 1 // offset integer size
	trailer[7] = 1 // object reference size
	binary.BigEndian.PutUint64(trailer[8:16], uint64(len(objects)))
	binary.BigEndian.PutUint64(trailer[16:24], 0)
	binary.BigEndian.PutUint64(trailer[24:32], uint64(offsetTableFrom))
	return append(raw, trailer...)
}

func qqMusicMacTestASCIIPlistString(value string) []byte {
	if len(value) < 15 {
		return append([]byte{0x50 | byte(len(value))}, value...)
	}
	return append([]byte{0x5f, 0x10, byte(len(value))}, value...)
}
