package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindLyricsPrefersExactMatch(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "Song.lrc"), "exact")
	mustWrite(t, filepath.Join(dir, "Song translated.lrc"), "translated")

	got, err := findLyrics(dir, "Song", `{name}.*\.lrc`)
	if err != nil {
		t.Fatalf("findLyrics returned error: %v", err)
	}
	if got != filepath.Join(dir, "Song.lrc") {
		t.Fatalf("got %q, want exact match", got)
	}
}

func TestFindLyricsRejectsAmbiguousLooseMatches(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "Song live.lrc"), "live")
	mustWrite(t, filepath.Join(dir, "Song translated.lrc"), "translated")

	got, err := findLyrics(dir, "Song", `{name}.*\.lrc`)
	if err == nil {
		t.Fatalf("expected ambiguity error, got path %q", got)
	}
	if !strings.Contains(err.Error(), "multiple matching lyrics files") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func mustWrite(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
}
