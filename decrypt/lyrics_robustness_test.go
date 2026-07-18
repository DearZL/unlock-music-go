package decrypt

import (
	"bytes"
	"strings"
	"testing"
)

func TestEmbedLyricsMP3HandlesV23ExtendedHeader(t *testing.T) {
	title, err := id3BuildFrame(3, "TIT2", []byte{0, 't', 'i', 't', 'l', 'e'})
	if err != nil {
		t.Fatal(err)
	}
	// v2.3's size excludes this four-byte size field: flags + padding = 6.
	extended := []byte{0, 0, 0, 6, 0, 0, 0, 0, 0, 0}
	in := testID3Tag(3, 0x40, append(extended, title...), false)
	in = append(in, 0xff, 0xfb, 0x90, 0x64)

	out, err := EmbedLyrics(in, "mp3", "line")
	if err != nil {
		t.Fatal(err)
	}
	if out[3] != 3 || out[5] != 0 {
		t.Fatalf("rebuilt v2.3 header = %x, want v2.3 without old flags", out[:10])
	}
	if !bytes.Contains(out, title) {
		t.Fatal("TIT2 frame was not preserved")
	}
	if got, err := DumpLyrics(out, "mp3"); err != nil || got != "line" {
		t.Fatalf("DumpLyrics = %q, %v", got, err)
	}
}

func TestEmbedLyricsMP3HandlesV24ExtendedHeaderAndFooter(t *testing.T) {
	title, err := id3BuildFrame(4, "TIT2", []byte{3, 't', 'i', 't', 'l', 'e'})
	if err != nil {
		t.Fatal(err)
	}
	// v2.4's extended-header size includes the size field itself.
	extended := []byte{0, 0, 0, 6, 1, 0}
	in := append(testID3Tag(4, 0x50, append(extended, title...), true), 0xff, 0xfb, 0x90, 0x64)

	out, err := EmbedLyrics(in, "mp3", "v24")
	if err != nil {
		t.Fatal(err)
	}
	if out[3] != 4 || out[5] != 0 {
		t.Fatalf("rebuilt v2.4 header = %x, want v2.4 without old flags", out[:10])
	}
	if !bytes.Contains(out, title) {
		t.Fatal("v2.4 TIT2 frame was not preserved")
	}
	if got, err := DumpLyrics(out, "mp3"); err != nil || got != "v24" {
		t.Fatalf("DumpLyrics = %q, %v", got, err)
	}
}

func TestEmbedLyricsMP3PreservesV22Frames(t *testing.T) {
	title, err := id3BuildFrame(2, "TT2", []byte{0, 'o', 'l', 'd'})
	if err != nil {
		t.Fatal(err)
	}
	in := append(testID3Tag(2, 0, title, false), 0xff, 0xfb, 0x90, 0x64)
	out, err := EmbedLyrics(in, "mp3", "v22")
	if err != nil {
		t.Fatal(err)
	}
	if out[3] != 2 || !bytes.Contains(out, title) || !bytes.Contains(out, []byte("ULT")) {
		t.Fatal("v2.2 tag was not preserved and extended with ULT")
	}
	if got, err := DumpLyrics(out, "mp3"); err != nil || got != "v22" {
		t.Fatalf("DumpLyrics = %q, %v", got, err)
	}
}

func TestEmbedLyricsMP3NormalisesTagWideUnsynchronisation(t *testing.T) {
	// The stored frame payload has an inserted 00 after FF.  Its frame size is
	// the stored (unsynchronised) byte count.
	title, err := id3BuildFrame(3, "TIT2", []byte{0, 'x', 0xff, 0x00, 0xe0})
	if err != nil {
		t.Fatal(err)
	}
	in := append(testID3Tag(3, 0x80, title, false), 0xff, 0xfb, 0x90, 0x64)
	out, err := EmbedLyrics(in, "mp3", "normalised")
	if err != nil {
		t.Fatal(err)
	}
	if out[5]&0x80 != 0 {
		t.Fatal("rebuilt tag retained global unsynchronisation flag")
	}
	parsed, present, err := id3ReadTag(out, nil)
	if err != nil || !present {
		t.Fatalf("id3ReadTag = present:%v err:%v", present, err)
	}
	if len(parsed.frames) < 2 || !bytes.Contains(parsed.frames[0], []byte{0xff, 0xe0}) {
		t.Fatal("retained frame was not de-unsynchronised")
	}
}

func TestEmbedLyricsMP3RejectsTruncatedExtendedHeader(t *testing.T) {
	in := testID3Tag(3, 0x40, []byte{0, 0, 0, 6}, false)
	if _, err := EmbedLyrics(in, "mp3", "line"); err == nil {
		t.Fatal("truncated extended header was accepted")
	}
}

func TestEmbedCoverMP3UsesV22PICFrame(t *testing.T) {
	in := append(testID3Tag(2, 0, nil, false), 0xff, 0xfb, 0x90, 0x64)
	out, err := EmbedCover(in, "mp3", testPNG)
	if err != nil {
		t.Fatal(err)
	}
	if out[3] != 2 || !bytes.Contains(out, []byte("PIC")) {
		t.Fatal("v2.2 cover was not written as a PIC frame")
	}
}

func TestEmbedLyricsOGGFindsCommentAfterAnotherPacket(t *testing.T) {
	comment := append([]byte("OpusTags"), flacVCSerialise("vendor", []string{"TITLE=song"})...)
	ident := []byte("OpusHead\x01\x02")
	in := serialiseOggPage(oggPage{
		headerType: 0x02, serial: 7, seqno: 0,
		lacing: []byte{byte(len(ident)), byte(len(comment))},
		body:   append(append([]byte(nil), ident...), comment...),
	})
	out, err := EmbedLyrics(in, "ogg", "same-page")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, ident) {
		t.Fatal("packet preceding OpusTags was lost")
	}
	if got, err := DumpLyrics(out, "ogg"); err != nil || got != "same-page" {
		t.Fatalf("DumpLyrics = %q, %v", got, err)
	}
}

func TestEmbedLyricsOGGHandlesInterleavedLogicalStreams(t *testing.T) {
	comment := append([]byte("OpusTags"), flacVCSerialise("vendor", []string{"TITLE=" + strings.Repeat("a", 300)})...)
	first, rest := comment[:255], comment[255:]
	other := serialiseOggPage(oggPage{
		headerType: 0x02, serial: 99, seqno: 0, lacing: []byte{4}, body: []byte("side"),
	})
	in := append(serialiseOggPage(oggPage{
		headerType: 0x02, serial: 7, seqno: 0, lacing: []byte{255}, body: first,
	}), other...)
	in = append(in, serialiseOggPage(oggPage{
		headerType: 0x01, serial: 7, seqno: 1, lacing: []byte{byte(len(rest))}, body: rest,
	})...)
	in = append(in, serialiseOggPage(oggPage{
		serial: 7, seqno: 2, lacing: []byte{5}, body: []byte("audio"),
	})...)

	out, err := EmbedLyrics(in, "ogg", "interleaved")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := DumpLyrics(out, "ogg"); err != nil || got != "interleaved" {
		t.Fatalf("DumpLyrics = %q, %v", got, err)
	}
	pages, err := oggParsePages(out)
	if err != nil {
		t.Fatal(err)
	}
	wantSeq := uint32(0)
	foundOther := false
	for _, page := range pages {
		switch page.serial {
		case 7:
			if page.seqno != wantSeq {
				t.Fatalf("serial 7 sequence = %d, want %d", page.seqno, wantSeq)
			}
			wantSeq++
		case 99:
			foundOther = bytes.Equal(page.body, []byte("side"))
		}
	}
	if !foundOther || !bytes.Contains(out, []byte("audio")) {
		t.Fatal("interleaved stream or target audio page was lost")
	}
}

func TestEmbedLyricsOGGPreservesPacketTail(t *testing.T) {
	comment := append([]byte("OpusTags"), flacVCSerialise("vendor", nil)...)
	tail := []byte("audio-packet")
	in := serialiseOggPage(oggPage{
		headerType: 0x02, serial: 3, seqno: 0,
		lacing: []byte{byte(len(comment)), byte(len(tail))},
		body:   append(append([]byte(nil), comment...), tail...),
	})
	out, err := EmbedLyrics(in, "ogg", "tail")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, tail) {
		t.Fatal("packet after comment header was lost")
	}
	if got, err := DumpLyrics(out, "ogg"); err != nil || got != "tail" {
		t.Fatalf("DumpLyrics = %q, %v", got, err)
	}
}

func TestFlacCommentParserRejectsTruncatedComment(t *testing.T) {
	data := []byte{
		0, 0, 0, 0, // empty vendor
		1, 0, 0, 0, // one comment
		10, 0, 0, 0,
		'x',
	}
	if _, err := flacVCAddLyrics(data, "line"); err == nil {
		t.Fatal("truncated Vorbis comment was accepted")
	}
}

func TestEmbedLyricsFLACRejectsMissingLastMetadataBlock(t *testing.T) {
	in := append([]byte("fLaC"), 0x00, 0x00, 0x00, 0x22)
	in = append(in, make([]byte, 34)...)
	if _, err := EmbedLyrics(in, "flac", "line"); err == nil {
		t.Fatal("unterminated FLAC metadata was accepted")
	}
}

func testID3Tag(major, flags byte, body []byte, footer bool) []byte {
	var out bytes.Buffer
	out.WriteString("ID3")
	out.WriteByte(major)
	out.WriteByte(0)
	out.WriteByte(flags)
	size := [4]byte{}
	encodeSyncsafe(size[:], uint32(len(body)))
	out.Write(size[:])
	out.Write(body)
	if footer {
		out.WriteString("3DI")
		out.WriteByte(major)
		out.WriteByte(0)
		out.WriteByte(flags)
		out.Write(size[:])
	}
	return out.Bytes()
}
