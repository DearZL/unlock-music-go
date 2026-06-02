package decrypt

// ncm.go — Netease Cloud Music (.ncm) decryption
//
// File layout:
//   [0x00] 8 bytes  – magic header "CTENFDАМ" (0x43 54 45 4E 46 44 41 4D)
//   [0x08] 2 bytes  – padding
//   [0x0A] 4 bytes  – key data length (little-endian uint32)
//   [0x0E] N bytes  – key data (each byte XOR 0x64, then AES-ECB CORE_KEY)
//          4 bytes  – metadata length
//          M bytes  – metadata (each byte XOR 0x63, skip first 22 bytes "163 key(Don't modify)",
//                     base64-decode the rest, then AES-ECB META_KEY, result is "music:{json}" or "dj:{json}")
//          5 bytes  – CRC / image version padding
//          4 bytes  – cover frame length
//          4 bytes  – image data length
//          I bytes  – image data
//          padding  – cover frame padding, if any
//          ...      – audio data (each byte XOR keyBox[i & 0xFF])

import (
	"bytes"
	"crypto/aes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"
)

var (
	ncmMagicHeader = []byte{0x43, 0x54, 0x45, 0x4E, 0x46, 0x44, 0x41, 0x4D}
	ncmCoreKey     = []byte{0x68, 0x7a, 0x48, 0x52, 0x41, 0x6d, 0x73, 0x6f, 0x35, 0x6b, 0x49, 0x6e, 0x62, 0x61, 0x78, 0x57}
	ncmMetaKey     = []byte{0x23, 0x31, 0x34, 0x6C, 0x6A, 0x6B, 0x5F, 0x21, 0x5C, 0x5D, 0x26, 0x30, 0x55, 0x3C, 0x27, 0x28}
)

// NcmMeta holds the metadata extracted from the NCM file.
type NcmMeta struct {
	MusicName string          `json:"musicName"`
	Artist    [][]interface{} `json:"artist"`
	Album     string          `json:"album"`
	AlbumPic  string          `json:"albumPic"`
	Format    string          `json:"format"`
}

// NcmDjMeta holds the DJ metadata format.
type NcmDjMeta struct {
	MainMusic NcmMeta `json:"mainMusic"`
}

// NcmResult is the result of decrypting a .ncm file.
type NcmResult struct {
	Audio []byte
	Ext   string
	Mime  string
	Meta  NcmMeta
	Cover []byte
}

// DecryptNcm decrypts a .ncm file and returns the raw audio bytes plus metadata.
func DecryptNcm(data []byte) (*NcmResult, error) {
	if len(data) < 10 {
		return nil, errors.New("ncm: file too short")
	}
	if !bytes.HasPrefix(data, ncmMagicHeader) {
		return nil, errors.New("ncm: invalid magic header")
	}

	offset := 10 // skip 8-byte magic + 2-byte padding

	// --- key data ---
	if offset+4 > len(data) {
		return nil, errors.New("ncm: file too short (key length)")
	}
	keyLen := int(binary.LittleEndian.Uint32(data[offset : offset+4]))
	offset += 4
	if offset+keyLen > len(data) {
		return nil, errors.New("ncm: file too short (key data)")
	}
	keyData := make([]byte, keyLen)
	for i := 0; i < keyLen; i++ {
		keyData[i] = data[offset+i] ^ 0x64
	}
	offset += keyLen

	plain, err := aesECBDecrypt(keyData, ncmCoreKey)
	if err != nil {
		return nil, errors.New("ncm: AES key decrypt failed: " + err.Error())
	}
	// strip 17-byte prefix "neteasecloudmusic"
	if len(plain) < 17 {
		return nil, errors.New("ncm: key plain text too short")
	}
	keyRaw := plain[17:]
	if len(keyRaw) == 0 {
		return nil, errors.New("ncm: empty audio key")
	}

	keyBox := buildKeyBox(keyRaw)

	// --- metadata ---
	if offset+4 > len(data) {
		return nil, errors.New("ncm: file too short (meta length)")
	}
	metaLen := int(binary.LittleEndian.Uint32(data[offset : offset+4]))
	offset += 4

	var meta NcmMeta
	if metaLen > 0 {
		if offset+metaLen > len(data) {
			return nil, errors.New("ncm: file too short (meta data)")
		}
		cipherMeta := make([]byte, metaLen)
		for i := 0; i < metaLen; i++ {
			cipherMeta[i] = data[offset+i] ^ 0x63
		}
		offset += metaLen

		// Skip the first 22 bytes "163 key(Don't modify):" prefix
		if len(cipherMeta) > 22 {
			b64Data := string(cipherMeta[22:])
			decoded, err := base64.StdEncoding.DecodeString(b64Data)
			if err == nil {
				plainMeta, err := aesECBDecrypt(decoded, ncmMetaKey)
				if err == nil {
					// format is "music:{...json...}" or "dj:{...json...}"
					s := string(plainMeta)
					idx := strings.Index(s, ":")
					if idx >= 0 {
						label := s[:idx]
						jsonStr := s[idx+1:]
						if label == "dj" {
							var djMeta NcmDjMeta
							if err := json.Unmarshal([]byte(jsonStr), &djMeta); err == nil {
								meta = djMeta.MainMusic
							}
						} else {
							json.Unmarshal([]byte(jsonStr), &meta) //nolint:errcheck
						}
					}
				}
			}
		}
	} else {
		offset += metaLen
	}

	cover, audioOffset, err := parseNcmCoverFrame(data, offset)
	if err != nil {
		return nil, err
	}
	offset = audioOffset

	// --- audio data: XOR with keyBox ---
	audio := make([]byte, len(data)-offset)
	copy(audio, data[offset:])
	for i := range audio {
		audio[i] ^= keyBox[i&0xFF]
	}

	ext := meta.Format
	if ext == "" {
		ext = SniffAudioExt(audio)
	}

	return &NcmResult{
		Audio: audio,
		Ext:   ext,
		Mime:  AudioMimeType(ext),
		Meta:  meta,
		Cover: cover,
	}, nil
}

func parseNcmCoverFrame(data []byte, offset int) ([]byte, int, error) {
	if offset+13 > len(data) {
		return nil, 0, errors.New("ncm: file too short (cover header)")
	}
	coverFrameLen := int(binary.LittleEndian.Uint32(data[offset+5 : offset+9]))
	imageLen := int(binary.LittleEndian.Uint32(data[offset+9 : offset+13]))
	if coverFrameLen < imageLen {
		return nil, 0, errors.New("ncm: invalid cover frame length")
	}
	imageStart := offset + 13
	imageEnd := imageStart + imageLen
	audioOffset := offset + 13 + coverFrameLen
	if imageEnd > len(data) || audioOffset > len(data) {
		return nil, 0, errors.New("ncm: file too short (cover data)")
	}
	cover := make([]byte, imageLen)
	copy(cover, data[imageStart:imageEnd])
	return cover, audioOffset, nil
}

// buildKeyBox generates the 256-byte decryption key box (RC4-like KSA + PRGA).
func buildKeyBox(key []byte) [256]byte {
	keyLen := len(key)
	var box [256]byte
	for i := range box {
		box[i] = byte(i)
	}
	j := 0
	for i := 0; i < 256; i++ {
		j = (int(box[i]) + j + int(key[i%keyLen])) & 0xFF
		box[i], box[j] = box[j], box[i]
	}

	// PRGA: build the final keyBox used for XOR
	var result [256]byte
	for i := 0; i < 256; i++ {
		ni := (i + 1) & 0xFF
		si := int(box[ni])
		sj := int(box[(ni+si)&0xFF])
		result[i] = box[(si+sj)&0xFF]
	}
	return result
}

// aesECBDecrypt decrypts data using AES-ECB with PKCS7 padding removal.
func aesECBDecrypt(cipherText, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	blockSize := block.BlockSize()
	if len(cipherText)%blockSize != 0 {
		return nil, errors.New("aes-ecb: ciphertext length is not a multiple of block size")
	}
	out := make([]byte, len(cipherText))
	for i := 0; i < len(cipherText); i += blockSize {
		block.Decrypt(out[i:i+blockSize], cipherText[i:i+blockSize])
	}
	// PKCS7 unpad
	if len(out) == 0 {
		return nil, errors.New("aes-ecb: empty plaintext")
	}
	padLen := int(out[len(out)-1])
	if padLen == 0 || padLen > blockSize {
		return nil, errors.New("aes-ecb: invalid padding")
	}
	for _, b := range out[len(out)-padLen:] {
		if int(b) != padLen {
			return nil, errors.New("aes-ecb: invalid padding")
		}
	}
	return out[:len(out)-padLen], nil
}
