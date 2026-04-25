package decrypt

// lyrics.go — Embed lyrics text into decoded audio files.
//
// Supported formats:
//   mp3  → ID3v2.3 USLT (Unsynchronised Lyric) frame
//   flac → Vorbis Comment block  LYRICS=<text>
//
// For both formats the implementation is self-contained (no external deps).

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// EmbedLyrics writes lyricsText into the audio bytes of the given format.
// It returns new (or modified) audio bytes.
func EmbedLyrics(audio []byte, ext, lyricsText string) ([]byte, error) {
	switch strings.ToLower(ext) {
	case "mp3":
		return embedLyricsMP3(audio, lyricsText)
	case "flac":
		return embedLyricsFLAC(audio, lyricsText)
	case "ogg":
		return embedLyricsFLAC(audio, lyricsText)
	default:
		return nil, fmt.Errorf("lyrics embedding is not supported for .%s files", ext)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// MP3 / ID3v2
// ──────────────────────────────────────────────────────────────────────────────

// embedLyricsMP3 adds (or replaces) the USLT frame inside an ID3v2.3 tag.
// Existing frames are preserved; any pre-existing USLT frames are removed first.
func embedLyricsMP3(audio []byte, lyrics string) ([]byte, error) {
	// Collect frames from existing ID3v2 tag (if any), excluding USLT.
	existingFrames, tagEnd := id3v2ParseFrames(audio)

	// Build the new USLT frame.
	usltFrame := id3v2BuildUSLT("XXX", lyrics)

	// Combine: existing frames + our USLT frame.
	var framesBuf bytes.Buffer
	for _, f := range existingFrames {
		framesBuf.Write(f)
	}
	framesBuf.Write(usltFrame)

	// Encode syncsafe tag size (frames only, no header).
	tagSize := framesBuf.Len()
	syncsafeSize := [4]byte{}
	encodeSyncsafe(syncsafeSize[:], uint32(tagSize))

	// Build full tag: 10-byte header + frames.
	var tag bytes.Buffer
	tag.WriteString("ID3")
	tag.WriteByte(0x03) // version 2.3
	tag.WriteByte(0x00) // revision
	tag.WriteByte(0x00) // flags
	tag.Write(syncsafeSize[:])
	tag.Write(framesBuf.Bytes())

	// Output: new tag + audio data (skip old tag if it was there).
	var out bytes.Buffer
	out.Write(tag.Bytes())
	out.Write(audio[tagEnd:])
	return out.Bytes(), nil
}

// id3v2ParseFrames returns all frames except USLT from an existing ID3v2 tag,
// and the byte offset where the tag ends (so the raw audio data starts there).
// If no ID3v2 tag is present, tagEnd = 0.
func id3v2ParseFrames(data []byte) (frames [][]byte, tagEnd int) {
	if len(data) < 10 || string(data[0:3]) != "ID3" {
		return nil, 0
	}
	version := data[3]
	if version != 3 && version != 4 {
		return nil, 0
	}
	tagSize := int(decodeSyncsafe(data[6:10]))
	tagEnd = 10 + tagSize
	if tagEnd > len(data) {
		tagEnd = len(data)
	}

	pos := 10
	for pos+10 <= tagEnd {
		if data[pos] == 0x00 { // padding
			break
		}
		frameID := string(data[pos : pos+4])
		var frameSize int
		if version == 4 {
			frameSize = int(decodeSyncsafe(data[pos+4 : pos+8]))
		} else {
			frameSize = int(binary.BigEndian.Uint32(data[pos+4 : pos+8]))
		}
		frameEnd := pos + 10 + frameSize
		if frameEnd > tagEnd {
			break
		}

		if frameID != "USLT" { // skip existing lyric frames
			raw := make([]byte, frameEnd-pos)
			copy(raw, data[pos:frameEnd])
			// Normalise frame size field to ID3v2.3 (big-endian, not syncsafe)
			// so all collected frames use the same version format.
			binary.BigEndian.PutUint32(raw[4:8], uint32(frameSize))
			frames = append(frames, raw)
		}
		pos = frameEnd
	}
	return frames, tagEnd
}

// id3v2BuildUSLT returns a serialised ID3v2.3 USLT frame.
func id3v2BuildUSLT(language, lyrics string) []byte {
	if len(language) < 3 {
		language = "XXX"
	}
	// Frame data: encoding(1) + language(3) + content_descriptor(1 null) + lyrics
	var data bytes.Buffer
	data.WriteByte(0x03)           // UTF-8
	data.WriteString(language[:3]) // 3-char language code
	data.WriteByte(0x00)           // content descriptor (empty, null-terminated)
	data.WriteString(lyrics)

	frameData := data.Bytes()

	// Frame header: id(4) + size(4 big-endian) + flags(2)
	frame := make([]byte, 10+len(frameData))
	copy(frame[0:4], "USLT")
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(frameData)))
	// flags: 0x00 0x00 (already zero)
	copy(frame[10:], frameData)
	return frame
}

// encodeSyncsafe writes n as a 4-byte ID3v2 syncsafe integer into dst.
func encodeSyncsafe(dst []byte, n uint32) {
	dst[3] = byte(n & 0x7F)
	n >>= 7
	dst[2] = byte(n & 0x7F)
	n >>= 7
	dst[1] = byte(n & 0x7F)
	n >>= 7
	dst[0] = byte(n & 0x7F)
}

// decodeSyncsafe reads a 4-byte ID3v2 syncsafe integer.
func decodeSyncsafe(b []byte) uint32 {
	return uint32(b[0])<<21 | uint32(b[1])<<14 | uint32(b[2])<<7 | uint32(b[3])
}

// ──────────────────────────────────────────────────────────────────────────────
// FLAC / Vorbis Comment
// ──────────────────────────────────────────────────────────────────────────────

// embedLyricsFLAC adds (or replaces) the LYRICS= entry in the
// VORBIS_COMMENT metadata block of a FLAC file.
func embedLyricsFLAC(audio []byte, lyrics string) ([]byte, error) {
	if len(audio) < 4 || string(audio[0:4]) != "fLaC" {
		return nil, errors.New("flac: invalid magic header")
	}

	// Walk metadata blocks to find VORBIS_COMMENT (type 4).
	// Each block: [is_last(1b) | type(7b)](1B) + size(3B big-endian) + data
	pos := 4
	vcBlockStart := -1 // byte offset of the VORBIS_COMMENT block header
	var vcBlockData []byte

	for pos+4 <= len(audio) {
		header := audio[pos]
		blockType := header & 0x7F
		isLast := (header >> 7) == 1
		blockSize := int(audio[pos+1])<<16 | int(audio[pos+2])<<8 | int(audio[pos+3])
		dataStart := pos + 4
		dataEnd := dataStart + blockSize

		if dataEnd > len(audio) {
			return nil, errors.New("flac: metadata block extends past end of file")
		}

		if blockType == 4 { // VORBIS_COMMENT
			vcBlockStart = pos
			vcBlockData = audio[dataStart:dataEnd]
			_ = isLast
			break
		}

		if isLast {
			break
		}
		pos = dataEnd
	}

	var newVCData []byte
	if vcBlockStart >= 0 {
		// Update existing Vorbis Comment block.
		var err error
		newVCData, err = flacVCAddLyrics(vcBlockData, lyrics)
		if err != nil {
			return nil, err
		}
	} else {
		// No Vorbis Comment block found — build a minimal one and insert it
		// right after the STREAMINFO block (first block, starting at byte 4).
		newVCData = flacVCBuild(lyrics)
		// We'll insert it; handle below.
	}

	if vcBlockStart >= 0 {
		// Reconstruct with replaced block.
		oldBlockSize := int(audio[vcBlockStart+1])<<16 |
			int(audio[vcBlockStart+2])<<8 | int(audio[vcBlockStart+3])
		oldHeader := audio[vcBlockStart]
		isLast := (oldHeader >> 7) << 7

		newHeader := [4]byte{
			isLast | 4, // keep is_last bit, type=4
			byte(len(newVCData) >> 16),
			byte(len(newVCData) >> 8),
			byte(len(newVCData)),
		}

		var out bytes.Buffer
		out.Write(audio[:vcBlockStart])
		out.Write(newHeader[:])
		out.Write(newVCData)
		out.Write(audio[vcBlockStart+4+oldBlockSize:])
		return out.Bytes(), nil
	}

	// Insert new VORBIS_COMMENT block after STREAMINFO (first metadata block).
	if len(audio) < 8 {
		return nil, errors.New("flac: file too short to insert metadata")
	}
	firstHeader := audio[4]
	firstBlockSize := int(audio[5])<<16 | int(audio[6])<<8 | int(audio[7])
	firstBlockEnd := 8 + firstBlockSize

	// The new VC block is NOT last; the existing first block's is_last bit is cleared.
	// Insert: [cleared first block] + [new VC block] + rest
	newFirstHeader := (firstHeader & 0x7F) // clear is_last
	// Determine is_last for the new VC block:
	// If the original first block WAS last, the new VC block becomes last.
	newVCIsLast := (firstHeader >> 7) << 7

	vcHeader := [4]byte{
		newVCIsLast | 4,
		byte(len(newVCData) >> 16),
		byte(len(newVCData) >> 8),
		byte(len(newVCData)),
	}

	var out bytes.Buffer
	out.WriteString("fLaC")
	out.WriteByte(newFirstHeader)
	out.Write(audio[5 : 4+firstBlockEnd]) // rest of first block header + data
	out.Write(vcHeader[:])
	out.Write(newVCData)
	out.Write(audio[4+firstBlockEnd:])
	return out.Bytes(), nil
}

// flacVCAddLyrics parses an existing Vorbis Comment block, removes any
// existing LYRICS comment, adds the new one, and returns the rebuilt block data.
func flacVCAddLyrics(data []byte, lyrics string) ([]byte, error) {
	if len(data) < 8 {
		return nil, errors.New("flac: vorbis comment block too short")
	}

	r := bytes.NewReader(data)

	// Vendor string
	var vendorLen uint32
	if err := binary.Read(r, binary.LittleEndian, &vendorLen); err != nil {
		return nil, errors.New("flac: cannot read vendor string length")
	}
	if int(vendorLen) > r.Len() {
		return nil, errors.New("flac: vendor string length exceeds block")
	}
	vendor := make([]byte, vendorLen)
	r.Read(vendor) //nolint:errcheck

	// Comment count
	var commentCount uint32
	if err := binary.Read(r, binary.LittleEndian, &commentCount); err != nil {
		return nil, errors.New("flac: cannot read comment count")
	}

	// Read existing comments, skipping LYRICS=
	var kept []string
	for i := uint32(0); i < commentCount; i++ {
		var cLen uint32
		if err := binary.Read(r, binary.LittleEndian, &cLen); err != nil {
			break
		}
		cData := make([]byte, cLen)
		r.Read(cData) //nolint:errcheck
		comment := string(cData)
		if !strings.HasPrefix(strings.ToUpper(comment), "LYRICS=") {
			kept = append(kept, comment)
		}
	}

	// Add new LYRICS comment
	kept = append(kept, "LYRICS="+lyrics)

	return flacVCSerialise(string(vendor), kept), nil
}

// flacVCBuild creates a minimal Vorbis Comment block with just a LYRICS entry.
func flacVCBuild(lyrics string) []byte {
	return flacVCSerialise("unlock-music-go", []string{"LYRICS=" + lyrics})
}

// flacVCSerialise encodes vendor string + comment list into Vorbis Comment block data.
func flacVCSerialise(vendor string, comments []string) []byte {
	var buf bytes.Buffer
	writeLE32 := func(n uint32) { binary.Write(&buf, binary.LittleEndian, n) } //nolint:errcheck

	writeLE32(uint32(len(vendor)))
	buf.WriteString(vendor)
	writeLE32(uint32(len(comments)))
	for _, c := range comments {
		writeLE32(uint32(len(c)))
		buf.WriteString(c)
	}
	return buf.Bytes()
}
