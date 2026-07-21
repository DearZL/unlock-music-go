//go:build !windows && !darwin

package decrypt

import "errors"

func resolveQQMusicEKey(_ QQMusicExInfo, _ QQMusicOptions) (string, error) {
	return "", errors.New("qqmusic/musicex: local key resolver is available on Windows and macOS; supply EKey")
}
