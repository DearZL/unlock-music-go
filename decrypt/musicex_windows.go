//go:build windows

package decrypt

import (
	"errors"
	"os"
	"path/filepath"
)

func resolveQQMusicEKey(info QQMusicExInfo, options QQMusicOptions) (string, error) {
	mmkvPath := options.MMKVPath
	if mmkvPath == "" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return "", errors.New("qqmusic/musicex: APPDATA is not set; specify -qqmusic-mmkv")
		}
		mmkvPath = filepath.Join(appData, "Tencent", "QQMusic", "Checkccae.dat")
	}

	deviceKey, err := qqMusicDeviceMMKVKey()
	if err != nil {
		return "", err
	}
	plainMMKV, err := decryptQQMusicMMKV(mmkvPath, deviceKey)
	if err != nil {
		return "", err
	}
	return findQQMusicEKey(plainMMKV, info.InnerName)
}
