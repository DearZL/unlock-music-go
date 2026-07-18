package decrypt

import (
	"errors"
	"fmt"
)

// decryptQQMusicPayloadPure applies the current QQ Music QMC Map/RC4 stream
// with the EncV2 key stored in Checkccae.dat. This stage has no DLL call.
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
