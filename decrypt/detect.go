package decrypt

import "bytes"

// AudioExt maps magic bytes to file extensions.
func SniffAudioExt(buf []byte) string {
	if len(buf) < 4 {
		return "mp3"
	}
	switch {
	case bytes.HasPrefix(buf, []byte{0x66, 0x4C, 0x61, 0x43}): // fLaC
		return "flac"
	case bytes.HasPrefix(buf, []byte{0x4F, 0x67, 0x67, 0x53}): // OggS
		return "ogg"
	case bytes.HasPrefix(buf, []byte{0x00, 0x00, 0x00, 0x20}) ||
		bytes.HasPrefix(buf, []byte{0x00, 0x00, 0x00, 0x1C}):
		return "m4a"
	case bytes.HasPrefix(buf, []byte{0x52, 0x49, 0x46, 0x46}): // RIFF
		return "wav"
	case bytes.HasPrefix(buf, []byte{0x30, 0x26, 0xB2, 0x75}): // WMA
		return "wma"
	case bytes.HasPrefix(buf, []byte{0x4D, 0x41, 0x43, 0x20}): // MAC (APE)
		return "ape"
	case bytes.HasPrefix(buf, []byte{0xFF, 0xFB}) ||
		bytes.HasPrefix(buf, []byte{0xFF, 0xF3}) ||
		bytes.HasPrefix(buf, []byte{0xFF, 0xF2}) ||
		bytes.HasPrefix(buf, []byte{0x49, 0x44, 0x33}): // ID3
		return "mp3"
	}
	return "mp3"
}

// AudioMimeType maps extension to MIME type.
func AudioMimeType(ext string) string {
	switch ext {
	case "flac":
		return "audio/flac"
	case "ogg":
		return "audio/ogg"
	case "m4a":
		return "audio/mp4"
	case "wav":
		return "audio/wav"
	case "wma":
		return "audio/x-ms-wma"
	case "ape":
		return "audio/ape"
	default:
		return "audio/mpeg"
	}
}
