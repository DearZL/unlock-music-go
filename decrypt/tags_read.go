package decrypt

// tags_read.go — Read and dump tag fields from MP3/FLAC/OGG files for verification.

import (
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf16"
)

// DumpLyrics reads the embedded lyrics from an MP3, FLAC, or OGG file and
// returns the lyrics text. Returns an error if no lyrics are found or the
// format is unsupported.
func DumpLyrics(data []byte, ext string) (string, error) {
	switch strings.ToLower(ext) {
	case "mp3":
		return dumpLyricsMP3(data)
	case "flac":
		return dumpLyricsFLAC(data)
	case "ogg":
		return dumpLyricsOGG(data)
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
			lyricsStart := id3LyricsTextStart(fd)
			if lyricsStart >= 0 && lyricsStart <= len(fd) {
				return decodeID3Text(fd[0], fd[lyricsStart:]), nil
			}
		}
		pos = dataEnd
	}
	return "", fmt.Errorf("no USLT (UNSYNCEDLYRICS) frame found in ID3v2 tag")
}

func id3LyricsTextStart(fd []byte) int {
	if len(fd) < 5 {
		return -1
	}
	encoding := fd[0]
	pos := 4 // skip encoding + language
	if encoding == 1 || encoding == 2 {
		for pos+1 < len(fd) {
			if fd[pos] == 0x00 && fd[pos+1] == 0x00 {
				return pos + 2
			}
			pos += 2
		}
		return -1
	}
	for pos < len(fd) {
		if fd[pos] == 0x00 {
			return pos + 1
		}
		pos++
	}
	return -1
}

func decodeID3Text(encoding byte, data []byte) string {
	switch encoding {
	case 1:
		if len(data) >= 2 {
			switch {
			case data[0] == 0xFF && data[1] == 0xFE:
				return decodeUTF16Text(data[2:], binary.LittleEndian)
			case data[0] == 0xFE && data[1] == 0xFF:
				return decodeUTF16Text(data[2:], binary.BigEndian)
			}
		}
		return decodeUTF16Text(data, binary.BigEndian)
	case 2:
		return decodeUTF16Text(data, binary.BigEndian)
	default:
		return string(data)
	}
}

func decodeUTF16Text(data []byte, order binary.ByteOrder) string {
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}
	codeUnits := make([]uint16, 0, len(data)/2)
	for i := 0; i < len(data); i += 2 {
		codeUnits = append(codeUnits, order.Uint16(data[i:i+2]))
	}
	return string(utf16.Decode(codeUnits))
}

// dumpLyricsOGG finds the LYRICS= tag in an OGG/Vorbis or OGG/Opus comment header.
func dumpLyricsOGG(data []byte) (string, error) {
	pages, err := oggParsePages(data)
	if err != nil {
		return "", fmt.Errorf("ogg: %w", err)
	}

	for i, page := range pages {
		if page.headerType&0x01 != 0 {
			continue
		}
		b := page.body
		var prefixLen int
		switch {
		case len(b) >= 7 && b[0] == 0x03 && string(b[1:7]) == "vorbis":
			prefixLen = 7
		case len(b) >= 8 && string(b[0:8]) == "OpusTags":
			prefixLen = 8
		default:
			continue
		}
		pkt, err := oggReassemblePacket(pages, i)
		if err != nil {
			return "", err
		}
		return dumpLyricsFromVC(pkt[prefixLen:])
	}
	return "", fmt.Errorf("ogg: no comment header found")
}

func oggReassemblePacket(pages []oggPage, first int) ([]byte, error) {
	var buf []byte
	for j := first; j < len(pages); j++ {
		p := pages[j]
		bodyOff := 0
		for _, segLen := range p.lacing {
			end := bodyOff + int(segLen)
			if end > len(p.body) {
				return nil, fmt.Errorf("ogg: segment exceeds page body")
			}
			buf = append(buf, p.body[bodyOff:end]...)
			bodyOff = end
			if segLen < 255 {
				return buf, nil
			}
		}
	}
	return nil, fmt.Errorf("ogg: comment packet not terminated")
}

// dumpLyricsFromVC extracts the LYRICS= value from raw Vorbis Comment data.
func dumpLyricsFromVC(data []byte) (string, error) {
	if len(data) < 8 {
		return "", fmt.Errorf("vorbis comment block too short")
	}
	vendorLen := int(binary.LittleEndian.Uint32(data[0:4]))
	off := 4 + vendorLen
	if off+4 > len(data) {
		return "", fmt.Errorf("vorbis comment: vendor string truncated")
	}
	count := int(binary.LittleEndian.Uint32(data[off : off+4]))
	off += 4
	for i := 0; i < count; i++ {
		if off+4 > len(data) {
			break
		}
		cLen := int(binary.LittleEndian.Uint32(data[off : off+4]))
		off += 4
		if off+cLen > len(data) {
			break
		}
		comment := string(data[off : off+cLen])
		off += cLen
		if strings.HasPrefix(strings.ToUpper(comment), "LYRICS=") {
			return comment[7:], nil
		}
	}
	return "", fmt.Errorf("no LYRICS field found in Vorbis Comment")
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
