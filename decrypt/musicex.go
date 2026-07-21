package decrypt

import "strings"

// QQMusicOptions selects the local key resolver for a current QQ Music
// musicex download.
//
// On Windows, MMKVPath is the optional Checkccae.dat override. On macOS,
// MacAppSupportPath is the optional QQMusicMac Application Support directory
// override; FilePath is the encrypted file's path and is used as the MMKV key.
// EKey bypasses the local cache and is useful for inspecting one captured key.
type QQMusicOptions struct {
	MMKVPath          string
	MacAppSupportPath string
	FilePath          string
	EKey              string
}

// DecryptQQMusicEx decrypts a recent musicex file. It parses the container,
// resolves its EncV2 key from the platform's local QQ Music cache (or EKey),
// then decrypts only the audio payload through the native Go QMC stream.
func DecryptQQMusicEx(data []byte, rawExt string, options QQMusicOptions) (*QmcResult, error) {
	info, err := ParseQQMusicEx(data)
	if err != nil {
		return nil, err
	}

	ekey := strings.TrimSpace(options.EKey)
	if ekey == "" {
		ekey, err = resolveQQMusicEKey(*info, options)
		if err != nil {
			return nil, err
		}
	}

	return decryptQQMusicPayloadPure(data[:info.DataLength], rawExt, ekey)
}
