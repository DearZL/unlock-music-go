package decrypt

// qmc.go — QQ Music (.qmc*, .mflac, .mgg, .bkcmp3, etc.) decryption
//
// File layout:
//   [audio data]
//   followed by one of:
//     - Nothing extra → use StaticCipher
//     - 4-byte little-endian keySize (< 0x400) + keySize bytes raw key → MapCipher or RC4Cipher
//     - "QTag" (4B) + rawKey + "," + songID + "," + extra (big-endian uint32 keySize at offset -8..-5)
//     - "STag" (4B) → error (no embedded key)

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strconv"
)

// QmcResult holds the decrypted audio and optional song ID.
type QmcResult struct {
	Audio  []byte
	SongID int64
	Ext    string
	Mime   string
}

// DecryptQmc decrypts a QQ Music file.
// rawExt is the original file extension (e.g. "mflac", "qmc0") used for format hints.
func DecryptQmc(data []byte, rawExt string) (*QmcResult, error) {
	d := &qmcDecoder{data: data, size: len(data)}
	if err := d.searchKey(); err != nil {
		return nil, err
	}
	if d.audioSize <= 0 {
		return nil, errors.New("qmc: invalid audio size")
	}

	audio := make([]byte, d.audioSize)
	copy(audio, data[:d.audioSize])
	d.cipher.Decrypt(audio, 0)

	// Determine extension hint from handler map
	extHint := qmcExtHint(rawExt)
	ext := SniffAudioExt(audio)
	if ext == "mp3" && extHint != "" {
		ext = extHint
	}

	return &QmcResult{
		Audio:  audio,
		SongID: d.songID,
		Ext:    ext,
		Mime:   AudioMimeType(ext),
	}, nil
}

// qmcExtHint returns the expected audio extension for a given QMC file extension.
func qmcExtHint(rawExt string) string {
	switch rawExt {
	case "mgg", "mgg0", "mggl", "mgg1", "qmc2", "qmc4", "qmc6", "qmc8", "bkcogg", "qmcogg":
		return "ogg"
	case "mflac", "mflac0", "qmcflac", "bkcflac":
		return "flac"
	case "mmp4", "bkcm4a", "tkm":
		return "m4a"
	case "bkcwav":
		return "wav"
	case "bkcape":
		return "ape"
	case "bkcwma":
		return "wma"
	default:
		return "mp3"
	}
}

type qmcDecoder struct {
	data      []byte
	size      int
	audioSize int
	songID    int64
	cipher    QmcStreamCipher
}

func (d *qmcDecoder) searchKey() error {
	last4 := d.data[d.size-4:]

	if bytes.Equal(last4, []byte("STag")) {
		return errors.New("qmc: STag found — no key embedded in file, cannot decrypt")
	}

	if bytes.Equal(last4, []byte("QTag")) {
		// QTag format: big-endian uint32 keySize at [-8..-5]
		sizeView := d.data[d.size-8 : d.size-4]
		keySize := int(binary.BigEndian.Uint32(sizeView))
		d.audioSize = d.size - keySize - 8

		rawKey := d.data[d.audioSize : d.size-8]
		// rawKey = <base64Key>,<songID>,<mediaID>
		commaIdx := bytes.IndexByte(rawKey, ',')
		if commaIdx < 0 {
			return errors.New("qmc: QTag: cannot find key/songID separator")
		}
		keyPart := rawKey[:commaIdx]
		rest := rawKey[commaIdx+1:]

		nextComma := bytes.IndexByte(rest, ',')
		var idPart []byte
		if nextComma >= 0 {
			idPart = rest[:nextComma]
		} else {
			idPart = rest
		}
		if id, err := strconv.ParseInt(string(idPart), 10, 64); err == nil {
			d.songID = id
		}

		return d.setCipher(keyPart)
	}

	// Default: last 4 bytes are little-endian uint32 key size
	keySize := int(binary.LittleEndian.Uint32(last4))
	if keySize < 0x400 {
		d.audioSize = d.size - keySize - 4
		rawKey := d.data[d.audioSize : d.size-4]
		return d.setCipher(rawKey)
	}

	// No key — use StaticCipher
	d.audioSize = d.size
	d.cipher = &QmcStaticCipher{}
	return nil
}

func (d *qmcDecoder) setCipher(rawKey []byte) error {
	keyDec, err := QmcDeriveKey(rawKey)
	if err != nil {
		return errors.New("qmc: key derivation failed: " + err.Error())
	}
	if len(keyDec) > 300 {
		d.cipher = NewQmcRC4Cipher(keyDec)
	} else {
		d.cipher = NewQmcMapCipher(keyDec)
	}
	return nil
}
