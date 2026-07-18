package decrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var musicExKeyPattern = regexp.MustCompile(`[A-Za-z0-9+/=]{100,}`)

// resolveQQMusicInstallDir finds the client installation required by the
// current device-key provider.
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

// decryptQQMusicMMKV decrypts the Checkccae.dat byte range that contains
// the MMKV records and their EncV2 keys.
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
	// The useful ekey records follow the initial CFB block. A zero IV matches
	// the record stream used by the desktop client and the original script.
	cipher.NewCFBDecrypter(block, make([]byte, aes.BlockSize)).XORKeyStream(plaintext, ciphertext)
	return plaintext, nil
}

// findQQMusicEKey selects the EncV2 string whose nearby record contains the
// internal musicex name. A single-record cache has an unambiguous fallback.
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
	return "", fmt.Errorf("qqmusic/mmkv: no ekey matches %q", innerName)
}
	match := matches[bestIndex]
	return string(mmkvPlain[match[0]:match[1]]), nil
}
