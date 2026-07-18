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
	tag, present, err := id3ReadTag(data, nil)
	if err != nil {
		return "", err
	}
	if !present {
		return "", fmt.Errorf("no ID3v2 tag found")
	}
	for _, frame := range tag.frames {
		idLen := 4
		if tag.major == 2 {
			idLen = 3
		}
		if id3CanonicalFrameID(tag.major, string(frame[:idLen])) != "USLT" {
			continue
		}
		fd, err := id3FramePayload(frame, tag.major)
		if err != nil {
			return "", err
		}
		if len(fd) < 5 {
			continue
		}
		// fd: encoding(1) + language(3) + descriptor(null-terminated) + lyrics
		lyricsStart := id3LyricsTextStart(fd)
		if lyricsStart >= 0 && lyricsStart <= len(fd) {
			return decodeID3Text(fd[0], fd[lyricsStart:]), nil
		}
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
	loc, err := oggFindCommentPacket(pages)
	if err != nil {
		return "", err
	}
	pkt := loc.packet
	switch {
	case len(pkt) >= 8 && string(pkt[:8]) == "OpusTags":
		return dumpLyricsFromVC(pkt[8:])
	case len(pkt) >= 8 && pkt[0] == 0x03 && string(pkt[1:7]) == "vorbis":
		if pkt[len(pkt)-1] != 0x01 {
			return "", fmt.Errorf("vorbis comment header has no framing bit")
		}
		return dumpLyricsFromVC(pkt[7 : len(pkt)-1])
	default:
		return "", fmt.Errorf("ogg: no comment header found")
	}
}

// dumpLyricsFromVC extracts the LYRICS= value from raw Vorbis Comment data.
func dumpLyricsFromVC(data []byte) (string, error) {
	_, comments, err := parseVorbisComment(data)
	if err != nil {
		return "", err
	}
	for _, comment := range comments {
		if strings.HasPrefix(strings.ToUpper(comment), "LYRICS=") {
			return comment[7:], nil
		}
	}
	return "", fmt.Errorf("no LYRICS field found in Vorbis Comment")
}

// dumpLyricsFLAC finds the LYRICS= comment in a FLAC Vorbis Comment block.
func dumpLyricsFLAC(data []byte) (string, error) {
	blocks, _, err := flacMetadataBlocks(data)
	if err != nil {
		return "", err
	}
	for _, block := range blocks {
		if block.typ == 4 {
			return dumpLyricsFromVC(data[block.start+4 : block.start+4+block.size])
		}
	}
	return "", fmt.Errorf("no Vorbis Comment block found in FLAC file")
}
