package decrypt

// musicex.go — QQ Music desktop "musicex" container support.
//
// Recent QQ Music desktop clients store a QMC-encrypted payload followed by a
// musicex footer.  Unlike older QMC files, the stream key is not embedded in
// the file: the matching EncV2 key is kept in the local Checkccae.dat MMKV
// cache.  The current stream decoder is provided by the installed
// CommonFunction.dll; legacy QMC files keep using the native Go historical
// decoder in qmc.go.

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf16"
)

const (
	musicExFooterSize  = 16
	musicExPayloadSize = 0xB0
)

var (
	musicExMagic      = []byte("musicex\x00")
	musicExKeyPattern = regexp.MustCompile(`[A-Za-z0-9+/=]{100,}`)
)

// QQMusicOptions specifies where a local QQ Music desktop installation and
// its encrypted download-key cache are located.  Empty fields select the
// normal Windows locations.
type QQMusicOptions struct {
	InstallDir string
	MMKVPath   string
}

// QQMusicExInfo describes the container footer used by recent QQ Music
// desktop downloads.
type QQMusicExInfo struct {
	DataLength   int
	FooterLength int
	InnerName    string
}

// IsQQMusicEx reports whether data carries a valid recent QQ Music musicex
// footer.  It is deliberately strict so old .mflac/.mgg files continue to
// take the legacy QMC path.
func IsQQMusicEx(data []byte) bool {
	_, err := ParseQQMusicEx(data)
	return err == nil
}

// HasQQMusicExFooter identifies the musicex container family using only its
// on-disk footer magic.  Call ParseQQMusicEx afterwards to enforce the
// supported format version.  Keeping these checks separate prevents a future
// musicex v2 file from accidentally falling through to the legacy QMC path.
func HasQQMusicExFooter(data []byte) bool {
	return len(data) >= musicExFooterSize && bytes.Equal(data[len(data)-8:], musicExMagic)
}

// ParseQQMusicEx reads the footer without decrypting the audio payload.
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

// DecryptQQMusicEx decrypts a recent musicex file.  The function obtains the
// per-device MMKV key from CommonFunction.dll, decrypts Checkccae.dat, matches
// the musicex internal name to its EncV2 key, then applies the existing QMC
// stream implementation to only the audio portion of the container.
func DecryptQQMusicEx(data []byte, rawExt string, options QQMusicOptions) (*QmcResult, error) {
	info, err := ParseQQMusicEx(data)
	if err != nil {
		return nil, err
	}

	mmkvPath := options.MMKVPath
	if mmkvPath == "" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return nil, errors.New("qqmusic/musicex: APPDATA is not set; specify -qqmusic-mmkv")
		}
		mmkvPath = filepath.Join(appData, "Tencent", "QQMusic", "Checkccae.dat")
	}

	installDir, err := resolveQQMusicInstallDir(options.InstallDir)
	if err != nil {
		return nil, err
	}
	deviceKey, err := qqMusicDeviceMMKVKey(installDir)
	if err != nil {
		return nil, err
	}
	plainMMKV, err := decryptQQMusicMMKV(mmkvPath, deviceKey)
	if err != nil {
		return nil, err
	}
	ekey, err := findQQMusicEKey(plainMMKV, info.InnerName)
	if err != nil {
		return nil, err
	}

	// The stream decoder is now reproduced by the native Go QMC Map/RC4
	// implementations.  installDir remains necessary here only for ordinal 12,
	// which derives the local MMKV cache key.
	return decryptQQMusicPayloadPure(data[:info.DataLength], rawExt, ekey)
}

func resolveQQMusicInstallDir(configured string) (string, error) {
	candidates := make([]string, 0, 4)
	if configured != "" {
		candidates = append(candidates, configured)
	}
	if envDir := os.Getenv("QQMUSIC_DIR"); envDir != "" {
		candidates = append(candidates, envDir)
	}
	candidates = append(candidates,
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Tencent", "QQMusic"),
		filepath.Join(os.Getenv("ProgramFiles"), "Tencent", "QQMusic"),
	)

	for _, dir := range candidates {
		if dir == "" {
			continue
		}
		if info, err := os.Stat(filepath.Join(dir, "CommonFunction.dll")); err == nil && !info.IsDir() {
			return dir, nil
		}
	}
	return "", errors.New("qqmusic/musicex: CommonFunction.dll not found; specify -qqmusic-dir")
}

func decryptQQMusicMMKV(path, deviceKey string) ([]byte, error) {
	if len(deviceKey) < aes.BlockSize {
		return nil, errors.New("qqmusic/mmkv: device key is shorter than 16 bytes")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("qqmusic/mmkv: read %s: %w", path, err)
	}
	if len(raw) < 8 {
		return nil, errors.New("qqmusic/mmkv: Checkccae.dat is too short")
	}
	size := int(binary.LittleEndian.Uint32(raw[:4]))
	if size <= 0 || size > len(raw)-4 {
		return nil, fmt.Errorf("qqmusic/mmkv: invalid actual size %d", size)
	}

	block, err := aes.NewCipher([]byte(deviceKey[:aes.BlockSize]))
	if err != nil {
		return nil, fmt.Errorf("qqmusic/mmkv: initialise AES: %w", err)
	}
	ciphertext := raw[4 : 4+size]
	plaintext := make([]byte, len(ciphertext))
	// This matches the desktop cache handling used by qqmusic_decode.ps1.  The
	// cache's useful ekey records occur after the initial CFB block, therefore
	// a zero IV preserves the record stream that is searched below.
	cipher.NewCFBDecrypter(block, make([]byte, aes.BlockSize)).XORKeyStream(plaintext, ciphertext)
	return plaintext, nil
}

func findQQMusicEKey(mmkvPlain []byte, innerName string) (string, error) {
	matches := musicExKeyPattern.FindAllIndex(mmkvPlain, -1)
	if len(matches) == 0 {
		return "", errors.New("qqmusic/mmkv: no EncV2 ekey strings found")
	}

	bestScore, bestIndex := -1, -1
	for i, match := range matches {
		start := match[0] - 256
		if start < 0 {
			start = 0
		}
		context := string(mmkvPlain[start:match[0]])
		score := 0
		if strings.Contains(context, innerName) {
			score = 10000 + len(innerName)
		} else {
			for offset := 0; offset <= len(innerName)-8; offset++ {
				suffix := innerName[offset:]
				if len(suffix) >= 8 && strings.Contains(context, suffix) && len(suffix) > score {
					score = len(suffix)
				}
			}
		}
		if score > bestScore {
			bestScore, bestIndex = score, i
		}
	}

	if bestScore <= 0 {
		if len(matches) == 1 {
			return string(mmkvPlain[matches[0][0]:matches[0][1]]), nil
		}
		return "", fmt.Errorf("qqmusic/mmkv: cannot match an ekey to %q", innerName)
	}
	match := matches[bestIndex]
	return string(mmkvPlain[match[0]:match[1]]), nil
}

// decryptQQMusicPayloadPure applies the current QQ Music QMC stream using the
// ekey stored in Checkccae.dat.
func decryptQQMusicPayloadPure(payload []byte, rawExt, ekey string) (*QmcResult, error) {
	keyDec, err := QmcDeriveKey([]byte(ekey))
	if err != nil {
		return nil, fmt.Errorf("qqmusic/musicex: derive payload key: %w", err)
	}

	audio := append([]byte(nil), payload...)
	var stream QmcStreamCipher
	if len(keyDec) > 300 {
		stream = NewQmcRC4Cipher(keyDec)
	} else {
		stream = NewQmcMapCipher(keyDec)
	}
	stream.Decrypt(audio, 0)
	if !HasKnownAudioMagic(audio) {
		return nil, errors.New("qqmusic/musicex: decrypted data has no recognised audio header; key or format version changed")
	}

	ext := SniffAudioExt(audio)
	if ext == "mp3" {
		ext = qmcExtHint(rawExt)
	}
	return &QmcResult{Audio: audio, Ext: ext, Mime: AudioMimeType(ext)}, nil
}
