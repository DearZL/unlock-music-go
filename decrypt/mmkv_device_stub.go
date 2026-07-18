//go:build !windows || !386

package decrypt

import "fmt"

func qqMusicDeviceMMKVKey(string) (string, error) {
	return "", fmt.Errorf("qqmusic/musicex: CommonFunction.dll is 32-bit; build this Windows tool with GOARCH=386")
}

func qqMusicDecryptPayload(string, []byte, string, string) (*QmcResult, error) {
	return nil, fmt.Errorf("qqmusic/musicex: CommonFunction.dll is 32-bit; build this Windows tool with GOARCH=386")
}
