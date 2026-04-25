package decrypt

// tags_read.go — Read and dump tag fields from MP3/FLAC files for verification.

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// DumpLyrics reads the embedded lyrics from an MP3 or FLAC file and returns
// the lyrics text. Returns an error if no lyrics are found or the format is
// unsupported.
func DumpLyrics(data []byte, ext string) (string, error) {
	switch strings.ToLower(ext) {
	case "mp3":
		return dumpLyricsMP3(data)
	case "flac":
		return dumpLyricsFLAC(data)
	default:
		return "", fmt.Errorf("tag reading not supported for .%s", ext)
	}
}

// dumpLyricsMP3 finds the first USLT frame in an ID3v2 tag and returns its text.
func dumpLyricsMP3(data []byte) (string, error) {
	if len(data) < 10 || string(data[0:3]) != "ID3" {
		return "", fmt.Errorf("no ID3v2 tag found")
	}
	version := data[3]
	if version != 3 && version != 4 {
		return "", fmt.Errorf("unsupported ID3v2 version: 2.%d", version)
	}
	tagSize := int(decodeSyncsafe(data[6:10]))
	tagEnd := 10 + tagSize
	if tagEnd > len(data) {
		tagEnd = len(data)
	}

	pos := 10
	for pos+10 <= tagEnd {
		if data[pos] == 0x00 {
			break
		}
		frameID := string(data[pos : pos+4])
		var frameSize int
		if version == 4 {
			frameSize = int(decodeSyncsafe(data[pos+4 : pos+8]))
		} else {
			frameSize = int(binary.BigEndian.Uint32(data[pos+4 : pos+8]))
		}
		dataStart := pos + 10
		dataEnd := dataStart + frameSize
		if dataEnd > tagEnd {
			break
		}

		if frameID == "USLT" && frameSize >= 5 {
			fd := data[dataStart:dataEnd]
			// fd: encoding(1) + language(3) + descriptor(null-terminated) + lyrics
			descEnd := 4 // skip encoding + language
			for descEnd < len(fd) && fd[descEnd] != 0x00 {
				descEnd++
			}
			descEnd++ // skip the null terminator
			if descEnd <= len(fd) {
				return string(fd[descEnd:]), nil
			}
		}
		pos = dataEnd
	}
	return "", fmt.Errorf("no USLT (UNSYNCEDLYRICS) frame found in ID3v2 tag")
}

// dumpLyricsFLAC finds the LYRICS= comment in a FLAC Vorbis Comment block.
func dumpLyricsFLAC(data []byte) (string, error) {
	if len(data) < 4 || string(data[0:4]) != "fLaC" {
		return "", fmt.Errorf("not a FLAC file")
	}
	pos := 4
	for pos+4 <= len(data) {
		header := data[pos]
		blockType := header & 0x7F
		isLast := (header >> 7) == 1
		blockSize := int(data[pos+1])<<16 | int(data[pos+2])<<8 | int(data[pos+3])
		dataStart := pos + 4
		dataEnd := dataStart + blockSize
		if dataEnd > len(data) {
			break
		}

		if blockType == 4 { // VORBIS_COMMENT
			bd := data[dataStart:dataEnd]
			if len(bd) < 8 {
				break
			}
			vendorLen := int(binary.LittleEndian.Uint32(bd[0:4]))
			if 4+vendorLen+4 > len(bd) {
				break
			}
			off := 4 + vendorLen
			count := int(binary.LittleEndian.Uint32(bd[off : off+4]))
			off += 4
			for i := 0; i < count; i++ {
				if off+4 > len(bd) {
					break
				}
				cLen := int(binary.LittleEndian.Uint32(bd[off : off+4]))
				off += 4
				if off+cLen > len(bd) {
					break
				}
				comment := string(bd[off : off+cLen])
				off += cLen
				if strings.HasPrefix(strings.ToUpper(comment), "LYRICS=") {
					return comment[7:], nil
				}
			}
			return "", fmt.Errorf("no LYRICS field found in Vorbis Comment block")
		}

		if isLast {
			break
		}
		pos = dataEnd
	}
	return "", fmt.Errorf("no Vorbis Comment block found in FLAC file")
}
