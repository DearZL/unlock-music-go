package decrypt

// Lyrics and tag helpers for MP3 (ID3v2), FLAC and Ogg/Vorbis/Opus.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
)

const (
	id3MaxTagSize       = 0x0fffffff
	flacMaxMetadataSize = 0x00ffffff
	oggUnknownGranule   = ^uint64(0)
)

// EmbedLyrics writes lyricsText into the audio bytes of the given format.
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

type id3Tag struct {
	major       byte
	frames      [][]byte
	audioOffset int
}

// embedLyricsMP3 preserves the input tag's major ID3 version.  This matters for
// v2.2 and v2.4 tags: their frame headers differ from v2.3.
func embedLyricsMP3(audio []byte, lyrics string) ([]byte, error) {
	tag, present, err := id3ReadTag(audio, map[string]bool{"USLT": true})
	if err != nil {
		return nil, err
	}
	if !present {
		tag = id3Tag{major: 3}
	}

	uslt, err := id3v2BuildUSLTForVersion(tag.major, "XXX", lyrics)
	if err != nil {
		return nil, err
	}
	tag.frames = append(tag.frames, uslt)
	return id3WriteTag(tag.major, tag.frames, audio[tag.audioOffset:])
}

// id3ReadTag reads frames while retaining unknown frames byte-for-byte where
// possible.  Extended headers and v2.4 footers are deliberately discarded when
// rebuilding: their checksums and padding describe the former tag layout.
func id3ReadTag(data []byte, exclude map[string]bool) (id3Tag, bool, error) {
	if len(data) < 3 || string(data[:3]) != "ID3" {
		return id3Tag{}, false, nil
	}
	if len(data) < 10 {
		return id3Tag{}, true, errors.New("id3: truncated tag header")
	}

	major := data[3]
	if major != 2 && major != 3 && major != 4 {
		return id3Tag{}, true, fmt.Errorf("id3: unsupported version 2.%d", major)
	}
	if data[4] == 0xff {
		return id3Tag{}, true, errors.New("id3: invalid revision")
	}
	if !isSyncsafe(data[6:10]) {
		return id3Tag{}, true, errors.New("id3: tag size is not syncsafe")
	}
	tagSize := int(decodeSyncsafe(data[6:10]))
	if tagSize > id3MaxTagSize || tagSize > len(data)-10 {
		return id3Tag{}, true, errors.New("id3: tag body is truncated")
	}

	flags := data[5]
	allowedFlags := byte(0xc0)
	if major == 3 {
		allowedFlags = 0xe0
	} else if major == 4 {
		allowedFlags = 0xf0
	}
	if flags&^allowedFlags != 0 {
		return id3Tag{}, true, fmt.Errorf("id3: unsupported tag flags 0x%02x", flags)
	}
	tagEnd := 10 + tagSize
	audioOffset := tagEnd
	if major == 4 && flags&0x10 != 0 {
		if len(data)-tagEnd < 10 {
			return id3Tag{}, true, errors.New("id3: footer is truncated")
		}
		if string(data[tagEnd:tagEnd+3]) != "3DI" {
			return id3Tag{}, true, errors.New("id3: invalid footer signature")
		}
		footer := data[tagEnd : tagEnd+10]
		if footer[3] != data[3] || footer[4] != data[4] || footer[5] != flags ||
			!bytes.Equal(footer[6:10], data[6:10]) {
			return id3Tag{}, true, errors.New("id3: footer does not match header")
		}
		audioOffset += 10
	}
	if major == 2 && flags&0x40 != 0 {
		return id3Tag{}, true, errors.New("id3: compressed v2.2 tags are not writable")
	}

	body := data[10:tagEnd]
	pos, err := id3FrameStart(body, major, flags)
	if err != nil {
		return id3Tag{}, true, err
	}
	frames, err := id3ParseFrames(body, pos, major, flags&0x80 != 0, exclude)
	if err != nil {
		return id3Tag{}, true, err
	}
	return id3Tag{major: major, frames: frames, audioOffset: audioOffset}, true, nil
}

func id3FrameStart(body []byte, major, flags byte) (int, error) {
	if flags&0x40 == 0 {
		return 0, nil
	}
	if major == 2 {
		return 0, errors.New("id3: v2.2 tag declares an extended header")
	}
	if len(body) < 4 {
		return 0, errors.New("id3: truncated extended header")
	}
	if major == 3 {
		size := int(binary.BigEndian.Uint32(body[:4]))
		if size < 6 || size > len(body)-4 {
			return 0, errors.New("id3: invalid v2.3 extended header size")
		}
		return size + 4, nil
	}
	if !isSyncsafe(body[:4]) {
		return 0, errors.New("id3: v2.4 extended header size is not syncsafe")
	}
	size := int(decodeSyncsafe(body[:4]))
	if size < 6 || size > len(body) {
		return 0, errors.New("id3: invalid v2.4 extended header size")
	}
	return size, nil
}

func id3ParseFrames(body []byte, pos int, major byte, tagUnsynchronised bool, exclude map[string]bool) ([][]byte, error) {
	headerLen := 10
	if major == 2 {
		headerLen = 6
	}

	var frames [][]byte
	for pos < len(body) {
		if body[pos] == 0 {
			if !allZero(body[pos:]) {
				return nil, errors.New("id3: non-zero data after frame padding")
			}
			break
		}
		if len(body)-pos < headerLen {
			return nil, errors.New("id3: truncated frame header")
		}

		idLen := 4
		if major == 2 {
			idLen = 3
		}
		frameID := string(body[pos : pos+idLen])
		if !isID3FrameID(body[pos : pos+idLen]) {
			return nil, fmt.Errorf("id3: invalid frame id %q", frameID)
		}

		var frameSize int
		switch major {
		case 2:
			frameSize = int(body[pos+3])<<16 | int(body[pos+4])<<8 | int(body[pos+5])
		case 3:
			frameSize = int(binary.BigEndian.Uint32(body[pos+4 : pos+8]))
		case 4:
			if !isSyncsafe(body[pos+4 : pos+8]) {
				return nil, fmt.Errorf("id3: frame %s has a non-syncsafe size", frameID)
			}
			frameSize = int(decodeSyncsafe(body[pos+4 : pos+8]))
		}
		if frameSize > len(body)-pos-headerLen {
			return nil, fmt.Errorf("id3: frame %s extends past tag end", frameID)
		}
		if frameSize == 0 {
			return nil, fmt.Errorf("id3: frame %s has an empty payload", frameID)
		}

		frameEnd := pos + headerLen + frameSize
		canonical := id3CanonicalFrameID(major, frameID)
		if !exclude[canonical] {
			raw := append([]byte(nil), body[pos:frameEnd]...)
			if tagUnsynchronised {
				raw = id3NormaliseTagUnsynchronisation(raw, major)
			}
			frames = append(frames, raw)
		}
		pos = frameEnd
	}
	return frames, nil
}

// id3NormaliseTagUnsynchronisation removes the tag-wide transform from an
// existing frame and rewrites its size.  The rebuilt tag has no global
// unsynchronisation flag, so retained frames stay readable.
func id3NormaliseTagUnsynchronisation(raw []byte, major byte) []byte {
	headerLen := 10
	if major == 2 {
		headerLen = 6
	}
	if len(raw) < headerLen {
		return raw
	}
	payload := id3Deunsynchronise(raw[headerLen:])
	out := make([]byte, headerLen+len(payload))
	copy(out, raw[:headerLen])
	copy(out[headerLen:], payload)
	switch major {
	case 2:
		out[3] = byte(len(payload) >> 16)
		out[4] = byte(len(payload) >> 8)
		out[5] = byte(len(payload))
	case 3:
		binary.BigEndian.PutUint32(out[4:8], uint32(len(payload)))
	case 4:
		encodeSyncsafe(out[4:8], uint32(len(payload)))
		// Frame-level unsynchronisation would otherwise request a second
		// de-unsynchronisation pass in a v2.4 reader.
		out[9] &^= 0x02
	}
	return out
}

func id3Deunsynchronise(data []byte) []byte {
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		out = append(out, data[i])
		if data[i] == 0xff && i+1 < len(data) && data[i+1] == 0x00 {
			i++
		}
	}
	return out
}

func id3WriteTag(major byte, frames [][]byte, audio []byte) ([]byte, error) {
	if major != 2 && major != 3 && major != 4 {
		return nil, fmt.Errorf("id3: unsupported version 2.%d", major)
	}
	var body bytes.Buffer
	for _, frame := range frames {
		body.Write(frame)
	}
	if body.Len() > id3MaxTagSize {
		return nil, errors.New("id3: rebuilt tag is too large")
	}

	var out bytes.Buffer
	out.WriteString("ID3")
	out.WriteByte(major)
	out.WriteByte(0)
	out.WriteByte(0) // no extended header, footer, or tag-wide unsynchronisation
	size := [4]byte{}
	encodeSyncsafe(size[:], uint32(body.Len()))
	out.Write(size[:])
	out.Write(body.Bytes())
	out.Write(audio)
	return out.Bytes(), nil
}

// id3v2ParseFramesExcluding is retained for internal callers that only need a
// best-effort frame list. Writers use id3ReadTag directly so malformed tags are
// reported instead of being overwritten.
func id3v2ParseFramesExcluding(data []byte, exclude map[string]bool) (frames [][]byte, tagEnd int) {
	tag, present, err := id3ReadTag(data, exclude)
	if err != nil || !present {
		return nil, 0
	}
	return tag.frames, tag.audioOffset
}

func id3v2BuildUSLT(language, lyrics string) []byte {
	frame, _ := id3v2BuildUSLTForVersion(3, language, lyrics)
	return frame
}

func id3v2BuildUSLTForVersion(major byte, language, lyrics string) ([]byte, error) {
	if len(language) < 3 {
		language = "XXX"
	}
	var data bytes.Buffer
	data.WriteByte(0x01)           // UTF-16 with BOM
	data.WriteString(language[:3]) // ISO-639-2
	data.Write([]byte{0x00, 0x00}) // empty UTF-16 descriptor
	data.Write(utf16LEWithBOM(lyrics))

	id := "USLT"
	if major == 2 {
		id = "ULT"
	}
	return id3BuildFrame(major, id, data.Bytes())
}

func id3BuildFrame(major byte, id string, payload []byte) ([]byte, error) {
	switch major {
	case 2:
		if len(id) != 3 || len(payload) > 0x00ffffff {
			return nil, errors.New("id3: invalid v2.2 frame")
		}
		out := make([]byte, 6+len(payload))
		copy(out[:3], id)
		out[3] = byte(len(payload) >> 16)
		out[4] = byte(len(payload) >> 8)
		out[5] = byte(len(payload))
		copy(out[6:], payload)
		return out, nil
	case 3, 4:
		if len(id) != 4 || len(payload) > id3MaxTagSize {
			return nil, errors.New("id3: invalid frame")
		}
		out := make([]byte, 10+len(payload))
		copy(out[:4], id)
		if major == 3 {
			binary.BigEndian.PutUint32(out[4:8], uint32(len(payload)))
		} else {
			encodeSyncsafe(out[4:8], uint32(len(payload)))
		}
		copy(out[10:], payload)
		return out, nil
	default:
		return nil, fmt.Errorf("id3: unsupported version 2.%d", major)
	}
}

func id3CanonicalFrameID(major byte, id string) string {
	if major != 2 {
		return id
	}
	switch id {
	case "ULT":
		return "USLT"
	case "PIC":
		return "APIC"
	default:
		return id
	}
}

func id3FramePayload(raw []byte, major byte) ([]byte, error) {
	headerLen := 10
	if major == 2 {
		headerLen = 6
	}
	if len(raw) < headerLen {
		return nil, errors.New("id3: short frame")
	}
	payload := raw[headerLen:]
	if major != 4 {
		return payload, nil
	}
	flags := raw[9]
	if flags&0x0c != 0 { // compression or encryption
		return nil, errors.New("id3: encrypted or compressed lyrics frame")
	}
	pos := 0
	if flags&0x40 != 0 { // grouping identity
		pos++
	}
	if flags&0x01 != 0 { // data length indicator
		pos += 4
	}
	if pos > len(payload) {
		return nil, errors.New("id3: truncated lyrics frame prefix")
	}
	payload = payload[pos:]
	if flags&0x02 != 0 {
		payload = id3Deunsynchronise(payload)
	}
	return payload, nil
}

func utf16LEWithBOM(s string) []byte {
	encoded := utf16.Encode([]rune(s))
	out := make([]byte, 2+len(encoded)*2)
	out[0], out[1] = 0xff, 0xfe
	for i, r := range encoded {
		binary.LittleEndian.PutUint16(out[2+i*2:], r)
	}
	return out
}

func encodeSyncsafe(dst []byte, n uint32) {
	dst[3] = byte(n & 0x7f)
	n >>= 7
	dst[2] = byte(n & 0x7f)
	n >>= 7
	dst[1] = byte(n & 0x7f)
	n >>= 7
	dst[0] = byte(n & 0x7f)
}

func decodeSyncsafe(b []byte) uint32 {
	return uint32(b[0])<<21 | uint32(b[1])<<14 | uint32(b[2])<<7 | uint32(b[3])
}

func isSyncsafe(b []byte) bool {
	return len(b) == 4 && b[0]&0x80 == 0 && b[1]&0x80 == 0 && b[2]&0x80 == 0 && b[3]&0x80 == 0
}

func isID3FrameID(b []byte) bool {
	for _, c := range b {
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

// ──────────────────────────────────────────────────────────────────────────────
// FLAC / Vorbis Comment
// ──────────────────────────────────────────────────────────────────────────────

type flacMetadataBlock struct {
	start int
	size  int
	typ   byte
	last  bool
}

func embedLyricsFLAC(audio []byte, lyrics string) ([]byte, error) {
	blocks, metadataEnd, err := flacMetadataBlocks(audio)
	if err != nil {
		return nil, err
	}

	var comment *flacMetadataBlock
	for i := range blocks {
		if blocks[i].typ == 4 {
			comment = &blocks[i]
			break
		}
	}

	if comment != nil {
		newData, err := flacVCAddLyrics(audio[comment.start+4:comment.start+4+comment.size], lyrics)
		if err != nil {
			return nil, err
		}
		if len(newData) > flacMaxMetadataSize {
			return nil, errors.New("flac: vorbis comment block is too large")
		}
		var out bytes.Buffer
		out.Write(audio[:comment.start])
		header := byte(4)
		if comment.last {
			header |= 0x80
		}
		out.WriteByte(header)
		out.Write([]byte{byte(len(newData) >> 16), byte(len(newData) >> 8), byte(len(newData))})
		out.Write(newData)
		out.Write(audio[comment.start+4+comment.size:])
		return out.Bytes(), nil
	}

	newData := flacVCBuild(lyrics)
	if len(newData) > flacMaxMetadataSize {
		return nil, errors.New("flac: vorbis comment block is too large")
	}
	first := blocks[0]
	var out bytes.Buffer
	out.WriteString("fLaC")
	out.WriteByte(first.typ) // inserting a new block means STREAMINFO is no longer last
	out.Write(audio[5 : first.start+4+first.size])
	header := byte(4)
	if first.last {
		header |= 0x80
	}
	out.WriteByte(header)
	out.Write([]byte{byte(len(newData) >> 16), byte(len(newData) >> 8), byte(len(newData))})
	out.Write(newData)
	out.Write(audio[first.start+4+first.size : metadataEnd])
	out.Write(audio[metadataEnd:])
	return out.Bytes(), nil
}

func flacMetadataBlocks(audio []byte) ([]flacMetadataBlock, int, error) {
	if len(audio) < 4 || string(audio[:4]) != "fLaC" {
		return nil, 0, errors.New("flac: invalid magic header")
	}
	pos := 4
	var blocks []flacMetadataBlock
	for {
		if len(audio)-pos < 4 {
			return nil, 0, errors.New("flac: metadata block header missing")
		}
		header := audio[pos]
		size := int(audio[pos+1])<<16 | int(audio[pos+2])<<8 | int(audio[pos+3])
		if size > len(audio)-(pos+4) {
			return nil, 0, errors.New("flac: metadata block extends past end of file")
		}
		block := flacMetadataBlock{start: pos, size: size, typ: header & 0x7f, last: header&0x80 != 0}
		blocks = append(blocks, block)
		pos += 4 + size
		if block.last {
			break
		}
	}
	if len(blocks) == 0 || blocks[0].typ != 0 || blocks[0].size != 34 {
		return nil, 0, errors.New("flac: first metadata block is not a valid STREAMINFO block")
	}
	return blocks, pos, nil
}

func flacVCAddLyrics(data []byte, lyrics string) ([]byte, error) {
	return flacVCAddOrReplace(data, "LYRICS", lyrics)
}

func flacVCAddOrReplace(data []byte, key, value string) ([]byte, error) {
	vendor, comments, err := parseVorbisComment(data)
	if err != nil {
		return nil, err
	}

	key = strings.ToUpper(key)
	if key == "" || strings.ContainsAny(key, "=\x00") {
		return nil, errors.New("vorbis comment: invalid field name")
	}
	prefix := key + "="
	kept := make([]string, 0, len(comments)+1)
	for _, text := range comments {
		if !strings.HasPrefix(strings.ToUpper(text), prefix) {
			kept = append(kept, text)
		}
	}
	kept = append(kept, key+"="+value)
	return flacVCSerialiseChecked(vendor, kept)
}

func parseVorbisComment(data []byte) (string, []string, error) {
	if len(data) < 8 {
		return "", nil, errors.New("vorbis comment: block too short")
	}
	off := 0
	readLength := func(label string) (uint32, error) {
		if len(data)-off < 4 {
			return 0, fmt.Errorf("vorbis comment: cannot read %s", label)
		}
		n := binary.LittleEndian.Uint32(data[off : off+4])
		off += 4
		return n, nil
	}
	readText := func(n uint32, label string) (string, error) {
		if uint64(n) > uint64(len(data)-off) {
			return "", fmt.Errorf("vorbis comment: %s length exceeds block", label)
		}
		end := off + int(n)
		text := string(data[off:end])
		off = end
		return text, nil
	}
	vendorLen, err := readLength("vendor length")
	if err != nil {
		return "", nil, err
	}
	vendor, err := readText(vendorLen, "vendor")
	if err != nil {
		return "", nil, err
	}
	count, err := readLength("comment count")
	if err != nil {
		return "", nil, err
	}
	if uint64(count) > uint64((len(data)-off)/4) {
		return "", nil, errors.New("vorbis comment: comment count exceeds block")
	}
	comments := make([]string, 0, count)
	for i := uint32(0); i < count; i++ {
		n, err := readLength("comment length")
		if err != nil {
			return "", nil, err
		}
		comment, err := readText(n, "comment")
		if err != nil {
			return "", nil, err
		}
		comments = append(comments, comment)
	}
	if off != len(data) {
		return "", nil, errors.New("vorbis comment: trailing bytes")
	}
	return vendor, comments, nil
}

func flacVCBuild(lyrics string) []byte {
	data, _ := flacVCSerialiseChecked("unlock-music-go", []string{"LYRICS=" + lyrics})
	return data
}

func flacVCSerialiseChecked(vendor string, comments []string) ([]byte, error) {
	if uint64(len(vendor)) > uint64(^uint32(0)) || uint64(len(comments)) > uint64(^uint32(0)) {
		return nil, errors.New("vorbis comment: value is too large")
	}
	total := uint64(8 + len(vendor))
	for _, c := range comments {
		if uint64(len(c)) > uint64(^uint32(0)) {
			return nil, errors.New("vorbis comment: field is too large")
		}
		total += 4 + uint64(len(c))
	}
	if total > uint64(^uint32(0)) {
		return nil, errors.New("vorbis comment: block is too large")
	}
	var buf bytes.Buffer
	writeLE32 := func(n uint32) { _ = binary.Write(&buf, binary.LittleEndian, n) }
	writeLE32(uint32(len(vendor)))
	buf.WriteString(vendor)
	writeLE32(uint32(len(comments)))
	for _, c := range comments {
		writeLE32(uint32(len(c)))
		buf.WriteString(c)
	}
	return buf.Bytes(), nil
}

// flacVCSerialise is kept for small internal test fixtures and callers that
// already control their input sizes.
func flacVCSerialise(vendor string, comments []string) []byte {
	data, err := flacVCSerialiseChecked(vendor, comments)
	if err != nil {
		return nil
	}
	return data
}

// ──────────────────────────────────────────────────────────────────────────────
// Ogg / Vorbis Comment
// ──────────────────────────────────────────────────────────────────────────────

type oggPage struct {
	headerType byte
	granule    uint64
	serial     uint32
	seqno      uint32
	lacing     []byte
	body       []byte
}

type oggPacketLocation struct {
	firstPage, firstSegment int
	lastPage, lastSegment   int // inclusive
	packet                  []byte
}

func embedLyricsOGG(audio []byte, lyrics string) ([]byte, error) {
	return oggModifyCommentTag(audio, "LYRICS", lyrics)
}

func oggModifyCommentTag(audio []byte, key, value string) ([]byte, error) {
	pages, err := oggParsePages(audio)
	if err != nil {
		return nil, fmt.Errorf("ogg: %w", err)
	}
	loc, err := oggFindCommentPacket(pages)
	if err != nil {
		return nil, err
	}
	newPacket, err := oggModifyComment(loc.packet, key, value)
	if err != nil {
		return nil, fmt.Errorf("ogg: %w", err)
	}

	first, last := pages[loc.firstPage], pages[loc.lastPage]
	beforeLacing, beforeBody := oggPageSlice(first, 0, loc.firstSegment)
	afterLacing, afterBody := oggPageSlice(last, loc.lastSegment+1, len(last.lacing))
	newPages, err := oggReplacePacketPages(first, last, beforeLacing, beforeBody, newPacket, afterLacing, afterBody)
	if err != nil {
		return nil, fmt.Errorf("ogg: %w", err)
	}

	removed := make(map[int]bool)
	removed[loc.firstPage] = true
	for i := loc.firstPage + 1; i <= loc.lastPage; i++ {
		if pages[i].serial == first.serial {
			removed[i] = true
		}
	}
	removedCount := 0
	for idx := range removed {
		_ = idx
		removedCount++
	}
	seqDelta := len(newPages) - removedCount

	var out bytes.Buffer
	for i, page := range pages {
		if i == loc.firstPage {
			for _, replacement := range newPages {
				out.Write(serialiseOggPage(replacement))
			}
		}
		if removed[i] {
			continue
		}
		if i > loc.lastPage && page.serial == first.serial && seqDelta != 0 {
			page.seqno = addOggSequenceDelta(page.seqno, seqDelta)
		}
		out.Write(serialiseOggPage(page))
	}
	return out.Bytes(), nil
}

func oggFindCommentPacket(pages []oggPage) (oggPacketLocation, error) {
	type partial struct {
		data                []byte
		firstPage, firstSeg int
		active              bool
	}
	partials := make(map[uint32]*partial)

	for pageIndex, page := range pages {
		state := partials[page.serial]
		if state == nil {
			state = &partial{}
			partials[page.serial] = state
		}
		continued := page.headerType&0x01 != 0
		if continued != state.active {
			return oggPacketLocation{}, fmt.Errorf("ogg: invalid continuation flag for serial %d", page.serial)
		}
		bodyOffset := 0
		for segIndex, segLen := range page.lacing {
			end := bodyOffset + int(segLen)
			if end > len(page.body) {
				return oggPacketLocation{}, errors.New("ogg: segment exceeds page body")
			}
			if !state.active {
				state.active = true
				state.firstPage, state.firstSeg = pageIndex, segIndex
				state.data = state.data[:0]
			}
			state.data = append(state.data, page.body[bodyOffset:end]...)
			bodyOffset = end
			if segLen < 255 {
				if isOggCommentPacket(state.data) {
					return oggPacketLocation{
						firstPage: state.firstPage, firstSegment: state.firstSeg,
						lastPage: pageIndex, lastSegment: segIndex,
						packet: append([]byte(nil), state.data...),
					}, nil
				}
				state.active = false
				state.data = state.data[:0]
			}
		}
	}
	return oggPacketLocation{}, errors.New("ogg: no comment header found")
}

func isOggCommentPacket(pkt []byte) bool {
	return (len(pkt) >= 8 && string(pkt[:8]) == "OpusTags") ||
		(len(pkt) >= 7 && pkt[0] == 0x03 && string(pkt[1:7]) == "vorbis")
}

func oggModifyComment(pkt []byte, key, value string) ([]byte, error) {
	var prefix, vcData []byte
	vorbis := false
	switch {
	case len(pkt) >= 8 && string(pkt[:8]) == "OpusTags":
		prefix, vcData = pkt[:8], pkt[8:]
	case len(pkt) >= 8 && pkt[0] == 0x03 && string(pkt[1:7]) == "vorbis":
		if pkt[len(pkt)-1] != 0x01 {
			return nil, errors.New("vorbis comment header has no framing bit")
		}
		prefix, vcData, vorbis = pkt[:7], pkt[7:len(pkt)-1], true
	default:
		return nil, errors.New("not a comment header packet")
	}
	newVC, err := flacVCAddOrReplace(vcData, key, value)
	if err != nil {
		return nil, err
	}
	result := append(append([]byte{}, prefix...), newVC...)
	if vorbis {
		result = append(result, 0x01)
	}
	return result, nil
}

func oggPageSlice(p oggPage, from, to int) ([]byte, []byte) {
	if from < 0 || to < from || to > len(p.lacing) {
		return nil, nil
	}
	start, end := 0, 0
	for i, n := range p.lacing {
		if i < from {
			start += int(n)
		}
		if i < to {
			end += int(n)
		}
	}
	return append([]byte(nil), p.lacing[from:to]...), append([]byte(nil), p.body[start:end]...)
}

// oggReplacePacketPages rebuilds only pages belonging to the comment packet's
// logical stream. Other streams may be interleaved at arbitrary page boundaries.
func oggReplacePacketPages(first, last oggPage, beforeLacing, beforeBody, packet, afterLacing, afterBody []byte) ([]oggPage, error) {
	if first.serial != last.serial {
		return nil, errors.New("ogg: comment packet changed logical streams")
	}
	if len(beforeLacing) > 0 && beforeLacing[len(beforeLacing)-1] == 255 {
		return nil, errors.New("ogg: comment packet starts before a prior packet is complete")
	}
	if len(afterLacing) > 0 && afterLacing[0] == 255 {
		return nil, errors.New("ogg: comment packet tail starts as a continuation")
	}

	pages := []oggPage{{
		headerType: first.headerType &^ 0x04, // EOS belongs on the rebuilt final page
		granule:    first.granule,
		serial:     first.serial,
		seqno:      first.seqno,
	}}
	appendSegment := func(seg byte, body []byte) {
		current := &pages[len(pages)-1]
		if len(current.lacing) == 255 {
			headerType := byte(0)
			if current.lacing[len(current.lacing)-1] == 255 {
				headerType = 0x01
			}
			pages = append(pages, oggPage{
				headerType: headerType, granule: oggUnknownGranule,
				serial: first.serial, seqno: first.seqno + uint32(len(pages)),
			})
			current = &pages[len(pages)-1]
		}
		current.lacing = append(current.lacing, seg)
		current.body = append(current.body, body...)
	}
	appendLaced := func(lacing, body []byte) error {
		off := 0
		for _, seg := range lacing {
			end := off + int(seg)
			if end > len(body) {
				return errors.New("ogg: lacing exceeds page body")
			}
			appendSegment(seg, body[off:end])
			off = end
		}
		if off != len(body) {
			return errors.New("ogg: page body has unreferenced bytes")
		}
		return nil
	}
	if err := appendLaced(beforeLacing, beforeBody); err != nil {
		return nil, err
	}
	if err := appendLaced(oggPacketLacing(packet), packet); err != nil {
		return nil, err
	}
	if err := appendLaced(afterLacing, afterBody); err != nil {
		return nil, err
	}
	if len(pages) == 0 || len(pages[len(pages)-1].lacing) == 0 {
		return nil, errors.New("ogg: comment replacement produced an empty page")
	}
	pages[len(pages)-1].granule = last.granule
	pages[len(pages)-1].headerType |= last.headerType & 0x04
	return pages, nil
}

func oggPacketLacing(pkt []byte) []byte {
	lacing := make([]byte, 0, len(pkt)/255+1)
	for len(pkt) >= 255 {
		lacing = append(lacing, 255)
		pkt = pkt[255:]
	}
	lacing = append(lacing, byte(len(pkt))) // includes zero terminator for exact multiples
	return lacing
}

func addOggSequenceDelta(seq uint32, delta int) uint32 {
	if delta >= 0 {
		return seq + uint32(delta)
	}
	return seq - uint32(-delta)
}

// oggBuildPacketPages is used by tests and retains the conventional standalone
// packet layout. Writers use oggReplacePacketPages to preserve adjacent packets.
func oggBuildPacketPages(pkt []byte, serial, firstSeqno uint32, granule uint64) []oggPage {
	lacing := oggPacketLacing(pkt)
	var pages []oggPage
	bodyOff := 0
	for len(lacing) > 0 {
		n := len(lacing)
		if n > 255 {
			n = 255
		}
		part := lacing[:n]
		bodyLen := 0
		for _, v := range part {
			bodyLen += int(v)
		}
		headerType := byte(0)
		if len(pages) > 0 && pages[len(pages)-1].lacing[len(pages[len(pages)-1].lacing)-1] == 255 {
			headerType = 0x01
		}
		pageGranule := oggUnknownGranule
		if n == len(lacing) {
			pageGranule = granule
		}
		pages = append(pages, oggPage{
			headerType: headerType, granule: pageGranule, serial: serial,
			seqno: firstSeqno + uint32(len(pages)), lacing: append([]byte(nil), part...),
			body: append([]byte(nil), pkt[bodyOff:bodyOff+bodyLen]...),
		})
		bodyOff += bodyLen
		lacing = lacing[n:]
	}
	return pages
}

func oggParsePages(data []byte) ([]oggPage, error) {
	var pages []oggPage
	for pos := 0; pos < len(data); {
		if len(data)-pos < 27 {
			return nil, errors.New("truncated page header")
		}
		if string(data[pos:pos+4]) != "OggS" {
			return nil, fmt.Errorf("lost sync at offset %d", pos)
		}
		if data[pos+4] != 0 {
			return nil, fmt.Errorf("unsupported stream structure version %d", data[pos+4])
		}
		nseg := int(data[pos+26])
		if len(data)-pos < 27+nseg {
			return nil, errors.New("truncated segment table")
		}
		lacing := append([]byte(nil), data[pos+27:pos+27+nseg]...)
		bodyLen := 0
		for _, s := range lacing {
			bodyLen += int(s)
		}
		headerLen := 27 + nseg
		if bodyLen > len(data)-pos-headerLen {
			return nil, errors.New("truncated page body")
		}
		raw := append([]byte(nil), data[pos:pos+headerLen+bodyLen]...)
		expectedCRC := binary.LittleEndian.Uint32(raw[22:26])
		for i := 22; i < 26; i++ {
			raw[i] = 0
		}
		if oggCRC32(raw) != expectedCRC {
			return nil, fmt.Errorf("checksum mismatch at offset %d", pos)
		}
		body := append([]byte(nil), data[pos+headerLen:pos+headerLen+bodyLen]...)
		pages = append(pages, oggPage{
			headerType: data[pos+5],
			granule:    binary.LittleEndian.Uint64(data[pos+6 : pos+14]),
			serial:     binary.LittleEndian.Uint32(data[pos+14 : pos+18]),
			seqno:      binary.LittleEndian.Uint32(data[pos+18 : pos+22]),
			lacing:     lacing,
			body:       body,
		})
		pos += headerLen + bodyLen
	}
	if len(pages) == 0 {
		return nil, errors.New("empty stream")
	}
	return pages, nil
}

func serialiseOggPage(p oggPage) []byte {
	if len(p.lacing) > 255 {
		panic("ogg page has more than 255 lacing values")
	}
	bodyLen := 0
	for _, n := range p.lacing {
		bodyLen += int(n)
	}
	if bodyLen != len(p.body) {
		panic("ogg page lacing/body mismatch")
	}
	headerLen := 27 + len(p.lacing)
	buf := make([]byte, headerLen+len(p.body))
	copy(buf[:4], "OggS")
	buf[4] = 0
	buf[5] = p.headerType
	binary.LittleEndian.PutUint64(buf[6:14], p.granule)
	binary.LittleEndian.PutUint32(buf[14:18], p.serial)
	binary.LittleEndian.PutUint32(buf[18:22], p.seqno)
	buf[26] = byte(len(p.lacing))
	copy(buf[27:headerLen], p.lacing)
	copy(buf[headerLen:], p.body)
	binary.LittleEndian.PutUint32(buf[22:26], oggCRC32(buf))
	return buf
}

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

func oggCRC32(data []byte) uint32 {
	var crc uint32
	for _, b := range data {
		crc = crc<<8 ^ oggCRC32Table[byte(crc>>24)^b]
	}
	return crc
}
