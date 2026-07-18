package main

import (
	"errors"
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

func TestTaskFailedControlsProcessStatus(t *testing.T) {
	if !taskFailed(taskResult{decryptErr: errors.New("decode")}, false) {
		t.Fatal("decrypt error must fail decrypt mode")
	}
	if !taskFailed(taskResult{writeErr: errors.New("write")}, false) {
		t.Fatal("write error must fail decrypt mode")
	}
	if taskFailed(taskResult{lrcErr: errors.New("lyrics")}, false) {
		t.Fatal("lyrics warning must not fail decrypt mode")
	}
	if !taskFailed(taskResult{lrcErr: errors.New("lyrics")}, true) {
		t.Fatal("lyrics error must fail embed mode")
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
