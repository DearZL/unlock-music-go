package decrypt

// tm.go — QQ Music iOS cache (.tm2 / .tm6) decryption
//
// The file is essentially an M4A (MP4 container) where the first 8 bytes
// have been zeroed out. The fix is simply to restore the correct M4A header.

var tmHeader = []byte{0x00, 0x00, 0x00, 0x20, 0x66, 0x74, 0x79, 0x70}

// TmResult holds the restored audio bytes.
type TmResult struct {
	Audio []byte
	Ext   string
	Mime  string
}

// DecryptTm restores a .tm2/.tm6 file to a valid M4A.
func DecryptTm(data []byte) (*TmResult, error) {
	audio := make([]byte, len(data))
	copy(audio, data)
	// Restore first 8 bytes
	for i := 0; i < 8 && i < len(audio); i++ {
		audio[i] = tmHeader[i]
	}
	return &TmResult{
		Audio: audio,
		Ext:   "m4a",
		Mime:  "audio/mp4",
	}, nil
}
