package decrypt

import (
	"strings"
	"testing"
)

func TestEmbedLyricsMP3RoundTripUTF16(t *testing.T) {
	audio := []byte{0xFF, 0xFB, 0x90, 0x64}
	lyrics := "[00:01.00]你好\n[00:02.00]世界"

	out, err := EmbedLyrics(audio, "mp3", lyrics)
	if err != nil {
		t.Fatalf("EmbedLyrics returned error: %v", err)
	}
	got, err := DumpLyrics(out, "mp3")
	if err != nil {
		t.Fatalf("DumpLyrics returned error: %v", err)
	}
	if got != lyrics {
		t.Fatalf("got %q, want %q", got, lyrics)
	}
}

func TestEmbedLyricsFLACInsertsVorbisCommentAfterStreamInfo(t *testing.T) {
	audio := append([]byte("fLaC"), 0x80, 0x00, 0x00, 0x22)
	audio = append(audio, make([]byte, 34)...)
	audio = append(audio, []byte("AUDIO")...)

	out, err := EmbedLyrics(audio, "flac", "line one")
	if err != nil {
		t.Fatalf("EmbedLyrics returned error: %v", err)
	}
	got, err := DumpLyrics(out, "flac")
	if err != nil {
		t.Fatalf("DumpLyrics returned error: %v", err)
	}
	if got != "line one" {
		t.Fatalf("got %q, want embedded lyrics", got)
	}
	if !strings.HasSuffix(string(out), "AUDIO") {
		t.Fatalf("FLAC payload suffix was not preserved")
	}
}

func TestDumpLyricsOGGReassemblesCrossPageCommentPacket(t *testing.T) {
	lyrics := strings.Repeat("a", 300)
	pkt := append([]byte("OpusTags"), flacVCSerialise("vendor", []string{"LYRICS=" + lyrics})...)
	first := pkt[:255]
	rest := pkt[255:]

	var ogg []byte
	ogg = append(ogg, serialiseOggPage(oggPage{
		headerType: 0x00,
		serial:     1,
		seqno:      0,
		lacing:     []byte{255},
		body:       first,
	})...)
	ogg = append(ogg, serialiseOggPage(oggPage{
		headerType: 0x01,
		serial:     1,
		seqno:      1,
		lacing:     []byte{byte(len(rest))},
		body:       rest,
	})...)

	got, err := DumpLyrics(ogg, "ogg")
	if err != nil {
		t.Fatalf("DumpLyrics returned error: %v", err)
	}
	if got != lyrics {
		t.Fatalf("got %d lyric bytes, want %d", len(got), len(lyrics))
	}
}

func TestDecryptQmcShortFileReturnsError(t *testing.T) {
	if _, err := DecryptQmc([]byte{1, 2, 3}, "qmc0"); err == nil {
		t.Fatal("expected short file error")
	}
}
