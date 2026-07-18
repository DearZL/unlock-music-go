package decrypt

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"
)

var testPNG = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 1, 2, 3, 4}

func TestParseNcmCoverFrame(t *testing.T) {
	data := make([]byte, 100)
	offset := 20
	coverFrameLen := uint32(12)
	imageLen := uint32(len(testPNG))
	binary.LittleEndian.PutUint32(data[offset+5:offset+9], coverFrameLen)
	binary.LittleEndian.PutUint32(data[offset+9:offset+13], imageLen)
	copy(data[offset+13:], testPNG)

	cover, audioOffset, err := parseNcmCoverFrame(data, offset)
	if err != nil {
		t.Fatalf("parseNcmCoverFrame returned error: %v", err)
	}
	if !bytes.Equal(cover, testPNG) {
		t.Fatalf("cover = %x, want %x", cover, testPNG)
	}
	if want := offset + 13 + int(coverFrameLen); audioOffset != want {
		t.Fatalf("audioOffset = %d, want %d", audioOffset, want)
	}
}

func TestEmbedCoverMP3WritesAPICAndLyricsPreservesIt(t *testing.T) {
	audio := []byte{0xFF, 0xFB, 0x90, 0x64}
	withCover, err := EmbedCover(audio, "mp3", testPNG)
	if err != nil {
		t.Fatalf("EmbedCover returned error: %v", err)
	}
	if !bytes.Contains(withCover, []byte("APIC")) || !bytes.Contains(withCover, testPNG) {
		t.Fatal("MP3 output does not contain APIC frame with image data")
	}

	withLyrics, err := EmbedLyrics(withCover, "mp3", "[00:00.00]line")
	if err != nil {
		t.Fatalf("EmbedLyrics returned error: %v", err)
	}
	if !bytes.Contains(withLyrics, []byte("APIC")) || !bytes.Contains(withLyrics, testPNG) {
		t.Fatal("lyrics embedding did not preserve APIC cover")
	}
}

func TestEmbedCoverFLACWritesPictureBlock(t *testing.T) {
	audio := append([]byte("fLaC"), 0x80, 0x00, 0x00, 0x22)
	audio = append(audio, make([]byte, 34)...)
	audio = append(audio, []byte("AUDIO")...)

	out, err := EmbedCover(audio, "flac", testPNG)
	if err != nil {
		t.Fatalf("EmbedCover returned error: %v", err)
	}

	pos := 4
	foundPicture := false
	for {
		header := out[pos]
		blockType := header & 0x7F
		blockSize := int(out[pos+1])<<16 | int(out[pos+2])<<8 | int(out[pos+3])
		dataStart := pos + 4
		dataEnd := dataStart + blockSize
		if blockType == 6 {
			foundPicture = bytes.Contains(out[dataStart:dataEnd], testPNG)
			break
		}
		pos = dataEnd
		if header&0x80 != 0 {
			break
		}
	}
	if !foundPicture {
		t.Fatal("FLAC output does not contain PICTURE block with image data")
	}
	if !strings.HasSuffix(string(out), "AUDIO") {
		t.Fatal("FLAC audio payload was not preserved")
	}
}

func TestEmbedCoverOGGWritesMetadataBlockPicture(t *testing.T) {
	pkt := append([]byte("OpusTags"), mustVorbisComment(t, "vendor", nil)...)
	ogg := serialiseOggPage(oggPage{
		headerType: 0x00,
		serial:     1,
		seqno:      0,
		lacing:     []byte{byte(len(pkt))},
		body:       pkt,
	})

	out, err := EmbedCover(ogg, "ogg", testPNG)
	if err != nil {
		t.Fatalf("EmbedCover returned error: %v", err)
	}

	pages, err := oggParsePages(out)
	if err != nil {
		t.Fatalf("oggParsePages returned error: %v", err)
	}
	vcData := pages[0].body[8:]
	comment := findVorbisCommentValue(t, vcData, "METADATA_BLOCK_PICTURE")
	picture, err := base64.StdEncoding.DecodeString(comment)
	if err != nil {
		t.Fatalf("cover comment is not base64: %v", err)
	}
	if !bytes.Contains(picture, testPNG) {
		t.Fatal("OGG picture comment does not contain image data")
	}
}

func findVorbisCommentValue(t *testing.T, data []byte, key string) string {
	t.Helper()
	vendorLen := int(binary.LittleEndian.Uint32(data[0:4]))
	off := 4 + vendorLen
	count := int(binary.LittleEndian.Uint32(data[off : off+4]))
	off += 4
	prefix := strings.ToUpper(key) + "="
	for i := 0; i < count; i++ {
		cLen := int(binary.LittleEndian.Uint32(data[off : off+4]))
		off += 4
		comment := string(data[off : off+cLen])
		off += cLen
		if strings.HasPrefix(strings.ToUpper(comment), prefix) {
			return comment[len(prefix):]
		}
	}
	t.Fatalf("comment %q not found", key)
	return ""
}
