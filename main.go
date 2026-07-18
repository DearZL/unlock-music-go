package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"unlock-music-go/decrypt"
)

func main() {
	inputPath := flag.String("i", "", "Input file or directory (required)")
	outputDir := flag.String("o", "", "Output directory (default: same as each source file)")
	lrcPattern := flag.String("lrc-pattern", `{name}\.lrc`, "Regex template for lyrics detection; {name} = song base name")
	dumpTags := flag.Bool("dump-tags", false, "Print embedded lyrics from a decoded MP3/FLAC/OGG file, then exit")
	embedLyrics := flag.Bool("embed-lyrics", false, "Embed lyrics into plain (already-decoded) MP3/FLAC/OGG files")
	withLyrics := flag.Bool("with-lyrics", false, "In decrypt mode, find a matching .lrc file and embed it into the decoded audio")
	qqMusicMMKV := flag.String("qqmusic-mmkv", "", "Path to QQ Music Checkccae.dat (needed only for recent musicex downloads)")
	flag.Usage = usage
	flag.Parse()

	if *inputPath == "" {
		fmt.Fprintln(os.Stderr, "Error: -i is required")
		flag.Usage()
		os.Exit(1)
	}

	// ── dump-tags mode ──────────────────────────────────────────────────────
	if *dumpTags {
		data, err := os.ReadFile(*inputPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(*inputPath), "."))
		lyrics, err := decrypt.DumpLyrics(data, ext)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		fmt.Println("=== Embedded lyrics ===")
		fmt.Println(lyrics)
		return
	}

	// ── validate lrc-pattern ────────────────────────────────────────────────
	testPattern := strings.ReplaceAll(*lrcPattern, "{name}", "test")
	if _, err := regexp.Compile("(?i)" + testPattern); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid -lrc-pattern %q: %v\n", *lrcPattern, err)
		os.Exit(1)
	}

	// ── embed-lyrics mode ───────────────────────────────────────────────────
	if *embedLyrics {
		if !runEmbedMode(*inputPath, *outputDir, *lrcPattern) {
			os.Exit(1)
		}
		return
	}

	// ── decrypt mode (default) ──────────────────────────────────────────────
	if !runDecryptMode(*inputPath, *outputDir, *lrcPattern, *withLyrics, decrypt.QQMusicOptions{
		MMKVPath: *qqMusicMMKV,
	}) {
		os.Exit(1)
	}
}
