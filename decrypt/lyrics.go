package decrypt

// lyrics.go — Embed lyrics text into decoded audio files.
//
// Supported formats:
//   mp3  → ID3v2.3 USLT (Unsynchronised Lyric) frame
//   flac → Vorbis Comment block  LYRICS=<text>
//   ogg  → Vorbis Comment (inside OGG page)  LYRICS=<text>

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
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
		return embedLyricsOGG(audio, lyricsText)
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
	existingFrames, tagEnd := id3v2ParseFramesExcluding(audio, map[string]bool{"USLT": true})

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
	return id3v2ParseFramesExcluding(data, map[string]bool{"USLT": true})
}

func id3v2ParseFramesExcluding(data []byte, exclude map[string]bool) (frames [][]byte, tagEnd int) {
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

		if !exclude[frameID] {
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
	// Frame data: encoding(1) + language(3) + UTF-16 content descriptor + lyrics
	var data bytes.Buffer
	data.WriteByte(0x01)           // UTF-16 with BOM, compatible with ID3v2.3
	data.WriteString(language[:3]) // 3-char language code
	data.Write([]byte{0x00, 0x00}) // empty UTF-16 descriptor, null-terminated
	data.Write(utf16LEWithBOM(lyrics))

	frameData := data.Bytes()

	// Frame header: id(4) + size(4 big-endian) + flags(2)
	frame := make([]byte, 10+len(frameData))
	copy(frame[0:4], "USLT")
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(frameData)))
	// flags: 0x00 0x00 (already zero)
	copy(frame[10:], frameData)
	return frame
}

func utf16LEWithBOM(s string) []byte {
	encoded := utf16.Encode([]rune(s))
	out := make([]byte, 2+len(encoded)*2)
	out[0], out[1] = 0xFF, 0xFE
	for i, r := range encoded {
		binary.LittleEndian.PutUint16(out[2+i*2:], r)
	}
	return out
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
		if len(newVCData) > 0xFFFFFF {
			return nil, errors.New("flac: vorbis comment block too large")
		}
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
	if firstBlockEnd > len(audio) {
		return nil, errors.New("flac: first metadata block extends past end of file")
	}
	if len(newVCData) > 0xFFFFFF {
		return nil, errors.New("flac: vorbis comment block too large")
	}

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
	out.Write(audio[5:firstBlockEnd]) // rest of first block header + data
	out.Write(vcHeader[:])
	out.Write(newVCData)
	out.Write(audio[firstBlockEnd:])
	return out.Bytes(), nil
}

// flacVCAddLyrics parses an existing Vorbis Comment block, removes any
// existing LYRICS comment, adds the new one, and returns the rebuilt block data.
func flacVCAddLyrics(data []byte, lyrics string) ([]byte, error) {
	return flacVCAddOrReplace(data, "LYRICS", lyrics)
}

func flacVCAddOrReplace(data []byte, key, value string) ([]byte, error) {
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

	key = strings.ToUpper(key)
	prefix := key + "="

	// Read existing comments, skipping the replaced key.
	var kept []string
	for i := uint32(0); i < commentCount; i++ {
		var cLen uint32
		if err := binary.Read(r, binary.LittleEndian, &cLen); err != nil {
			break
		}
		cData := make([]byte, cLen)
		r.Read(cData) //nolint:errcheck
		comment := string(cData)
		if !strings.HasPrefix(strings.ToUpper(comment), prefix) {
			kept = append(kept, comment)
		}
	}

	kept = append(kept, key+"="+value)

	return flacVCSerialise(string(vendor), kept), nil
}

// flacVCBuild creates a minimal Vorbis Comment block with just a LYRICS entry.
func flacVCBuild(lyrics string) []byte {
	return flacVCSerialise("unlock-music-go", []string{"LYRICS=" + lyrics})
}

// ──────────────────────────────────────────────────────────────────────────────
// OGG / Vorbis Comment
// ──────────────────────────────────────────────────────────────────────────────

// oggPage holds a parsed OGG page.
type oggPage struct {
	headerType byte
	granule    uint64
	serial     uint32
	seqno      uint32
	lacing     []byte // segment table
	body       []byte // page payload
}

// embedLyricsOGG adds (or replaces) the LYRICS= Vorbis Comment tag in an
// OGG/Vorbis or OGG/Opus file.
func embedLyricsOGG(audio []byte, lyrics string) ([]byte, error) {
	return oggModifyCommentTag(audio, "LYRICS", lyrics)
}

func oggModifyCommentTag(audio []byte, key, value string) ([]byte, error) {
	pages, err := oggParsePages(audio)
	if err != nil {
		return nil, fmt.Errorf("ogg: %w", err)
	}

	// Find the comment header packet.
	// It always starts on a non-continuation page whose body begins with
	// 0x03+"vorbis" (Vorbis) or "OpusTags" (Opus).
	commentFirst := -1
	commentLast := -1
	afterSeg := 0 // first segment index in commentLast NOT part of the comment packet
	var commentPkt []byte

	for i, page := range pages {
		if page.headerType&0x01 != 0 {
			continue // continuation page – packet cannot start here
		}
		b := page.body
		if !((len(b) >= 7 && b[0] == 0x03 && string(b[1:7]) == "vorbis") ||
			(len(b) >= 8 && string(b[0:8]) == "OpusTags")) {
			continue
		}
		commentFirst = i

		// Reassemble the comment packet (may span several pages).
		var buf []byte
		done := false
		for j := i; j < len(pages) && !done; j++ {
			p := pages[j]
			bodyOff := 0
			for si, segLen := range p.lacing {
				buf = append(buf, p.body[bodyOff:bodyOff+int(segLen)]...)
				bodyOff += int(segLen)
				if segLen < 255 {
					commentLast = j
					afterSeg = si + 1
					done = true
					break
				}
			}
		}
		if !done {
			return nil, errors.New("ogg: comment packet not terminated")
		}
		commentPkt = buf
		break
	}

	if commentFirst < 0 {
		return nil, errors.New("ogg: no comment header found")
	}

	// Modify the comment packet.
	newPkt, err := oggModifyComment(commentPkt, key, value)
	if err != nil {
		return nil, fmt.Errorf("ogg: %w", err)
	}

	// Segments in commentLast that follow the comment packet (start of next packet).
	tailLacing, tailBody := oggPageTail(pages[commentLast], afterSeg)

	// Build replacement page(s) for the modified comment packet.
	ref := pages[commentFirst]
	newPages := oggBuildPacketPages(newPkt, ref.serial, ref.seqno, ref.granule)

	// Append post-comment tail to the last new page (if it fits), or add a new page.
	if len(tailLacing) > 0 {
		last := &newPages[len(newPages)-1]
		if len(last.lacing)+len(tailLacing) <= 255 {
			last.lacing = append(last.lacing, tailLacing...)
			last.body = append(last.body, tailBody...)
		} else {
			newPages = append(newPages, oggPage{
				headerType: 0x00,
				granule:    pages[commentLast].granule,
				serial:     ref.serial,
				seqno:      ref.seqno + uint32(len(newPages)),
				lacing:     tailLacing,
				body:       tailBody,
			})
		}
	}

	// Adjust sequence numbers of all subsequent same-serial pages.
	seqDelta := len(newPages) - (commentLast - commentFirst + 1)

	var out bytes.Buffer
	for i := 0; i < commentFirst; i++ {
		out.Write(serialiseOggPage(pages[i]))
	}
	for _, p := range newPages {
		out.Write(serialiseOggPage(p))
	}
	for i := commentLast + 1; i < len(pages); i++ {
		p := pages[i]
		if p.serial == ref.serial && seqDelta != 0 {
			p.seqno = uint32(int(p.seqno) + seqDelta)
		}
		out.Write(serialiseOggPage(p))
	}
	return out.Bytes(), nil
}

// oggModifyComment returns a new comment header packet with a comment added/replaced.
func oggModifyComment(pkt []byte, key, value string) ([]byte, error) {
	var prefix, vcData []byte
	vorbis := false
	switch {
	case len(pkt) >= 7 && pkt[0] == 0x03 && string(pkt[1:7]) == "vorbis":
		prefix, vcData = pkt[:7], pkt[7:]
		vorbis = true
	case len(pkt) >= 8 && string(pkt[0:8]) == "OpusTags":
		prefix, vcData = pkt[:8], pkt[8:]
	default:
		return nil, errors.New("not a comment header packet")
	}
	// Vorbis Comment binary format is identical to FLAC's; reuse the FLAC helper.
	newVC, err := flacVCAddOrReplace(vcData, key, value)
	if err != nil {
		return nil, err
	}
	result := append(append([]byte{}, prefix...), newVC...)
	if vorbis {
		// Vorbis I spec §5.2.1 requires a framing bit (value 1) at the end of
		// the comment header packet.  OpusTags has no such requirement.
		result = append(result, 0x01)
	}
	return result, nil
}

// oggPageTail returns the lacing and body bytes of page p starting at segment index start.
func oggPageTail(p oggPage, start int) (lacing []byte, body []byte) {
	if start >= len(p.lacing) {
		return nil, nil
	}
	off := 0
	for _, s := range p.lacing[:start] {
		off += int(s)
	}
	return p.lacing[start:], p.body[off:]
}

// oggBuildPacketPages encodes a single OGG packet into one or more pages.
// The first page gets headerType 0x00; continuation pages get 0x01.
func oggBuildPacketPages(pkt []byte, serial, firstSeqno uint32, granule uint64) []oggPage {
	var pages []oggPage
	offset := 0
	seqno := firstSeqno
	first := true

	for {
		var lacing []byte
		bodyStart := offset

		for len(lacing) < 255 {
			remaining := len(pkt) - offset
			if remaining == 0 {
				if first { // empty packet: one zero-length terminating segment
					lacing = append(lacing, 0)
				}
				break
			}
			seg := remaining
			if seg > 255 {
				seg = 255
			}
			lacing = append(lacing, byte(seg))
			offset += seg
			if seg < 255 {
				break // packet terminated
			}
		}

		// When packet length is an exact multiple of 255 we need a zero-length
		// terminating segment to signal the end of the packet.
		if offset == len(pkt) && len(lacing) > 0 && lacing[len(lacing)-1] == 255 {
			if len(lacing) < 255 {
				lacing = append(lacing, 0)
			} else {
				// Page is full (255 × 255 bytes). Flush it, then emit a
				// single-segment terminating page.
				ht := byte(0x00)
				if !first {
					ht = 0x01
				}
				pages = append(pages, oggPage{
					headerType: ht, granule: granule, serial: serial,
					seqno: seqno, lacing: lacing, body: pkt[bodyStart:offset],
				})
				seqno++
				pages = append(pages, oggPage{
					headerType: 0x01, granule: granule, serial: serial,
					seqno: seqno, lacing: []byte{0}, body: nil,
				})
				return pages
			}
		}

		ht := byte(0x00)
		if !first {
			ht = 0x01
		}
		pages = append(pages, oggPage{
			headerType: ht, granule: granule, serial: serial,
			seqno: seqno, lacing: lacing, body: pkt[bodyStart:offset],
		})
		seqno++
		first = false

		if offset >= len(pkt) {
			break
		}
	}
	return pages
}

// oggParsePages splits raw OGG data into a slice of pages.
func oggParsePages(data []byte) ([]oggPage, error) {
	var pages []oggPage
	pos := 0
	for pos < len(data) {
		if pos+27 > len(data) {
			return nil, errors.New("truncated page header")
		}
		if string(data[pos:pos+4]) != "OggS" {
			return nil, fmt.Errorf("lost sync at offset %d", pos)
		}
		nseg := int(data[pos+26])
		if pos+27+nseg > len(data) {
			return nil, errors.New("truncated segment table")
		}
		lacing := make([]byte, nseg)
		copy(lacing, data[pos+27:pos+27+nseg])
		bodyLen := 0
		for _, s := range lacing {
			bodyLen += int(s)
		}
		headerLen := 27 + nseg
		if pos+headerLen+bodyLen > len(data) {
			return nil, errors.New("truncated page body")
		}
		body := make([]byte, bodyLen)
		copy(body, data[pos+headerLen:pos+headerLen+bodyLen])
		pages = append(pages, oggPage{
			headerType: data[pos+5],
			granule:    binary.LittleEndian.Uint64(data[pos+6:]),
			serial:     binary.LittleEndian.Uint32(data[pos+14:]),
			seqno:      binary.LittleEndian.Uint32(data[pos+18:]),
			lacing:     lacing,
			body:       body,
		})
		pos += headerLen + bodyLen
	}
	return pages, nil
}

// serialiseOggPage builds the raw bytes of a page and fills in the CRC checksum.
func serialiseOggPage(p oggPage) []byte {
	nseg := len(p.lacing)
	headerLen := 27 + nseg
	buf := make([]byte, headerLen+len(p.body))
	copy(buf[0:4], "OggS")
	buf[4] = 0 // stream structure version
	buf[5] = p.headerType
	binary.LittleEndian.PutUint64(buf[6:], p.granule)
	binary.LittleEndian.PutUint32(buf[14:], p.serial)
	binary.LittleEndian.PutUint32(buf[18:], p.seqno)
	// buf[22:26] = 0  (CRC placeholder — must be zero during computation)
	buf[26] = byte(nseg)
	copy(buf[27:], p.lacing)
	copy(buf[headerLen:], p.body)
	crc := oggCRC32(buf)
	binary.LittleEndian.PutUint32(buf[22:], crc)
	return buf
}

// oggCRC32Table is the lookup table for the OGG CRC-32 (polynomial 0x04c11db7).
var oggCRC32Table = func() [256]uint32 {
	var t [256]uint32
	for i := range t {
		crc := uint32(i) << 24
		for j := 0; j < 8; j++ {
			if crc&0x80000000 != 0 {
				crc = crc<<1 ^ 0x04c11db7
			} else {
				crc <<= 1
			}
		}
		t[i] = crc
	}
	return t
}()

// oggCRC32 computes the OGG page checksum (no pre/post inversion, initial value 0).
func oggCRC32(data []byte) uint32 {
	var crc uint32
	for _, b := range data {
		crc = crc<<8 ^ oggCRC32Table[byte(crc>>24)^b]
	}
	return crc
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
