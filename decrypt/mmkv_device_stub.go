//go:build !windows

package decrypt

import "fmt"

func qqMusicDeviceMMKVKey() (string, error) {
	return "", fmt.Errorf("qqmusic/musicex: automatic Checkccae.dat device-key derivation is available on Windows")
}
