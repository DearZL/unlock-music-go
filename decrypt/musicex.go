package decrypt

// musicex.go coordinates QQ Music desktop's current musicex container.

import (
	"errors"
	"os"
	"path/filepath"
)

// QQMusicOptions specifies the encrypted local download-key cache location.
// An empty path selects the normal Windows QQ Music location.
type QQMusicOptions struct {
	MMKVPath string
}

// DecryptQQMusicEx decrypts a recent musicex file. It parses the container,
// opens the encrypted local key cache, finds the matching EncV2 ekey, then
// decrypts only the audio payload through the native Go QMC stream.
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

	deviceKey, err := qqMusicDeviceMMKVKey()
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

	return decryptQQMusicPayloadPure(data[:info.DataLength], rawExt, ekey)
}
