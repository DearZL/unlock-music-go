package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"unlock-music-go/decrypt"
)

func runDecryptMode(inputPath, outputDir, lrcPattern string, withLyrics bool, qqMusicOptions decrypt.QQMusicOptions) {
	tasks, err := collectTasks(inputPath, encryptedExts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	if len(tasks) == 0 {
		fmt.Println("No supported encrypted files found.")
		return
	}

	if outputDir != "" {
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			fmt.Fprintln(os.Stderr, "Error creating output directory:", err)
			os.Exit(1)
		}
	}

	results := make([]taskResult, 0, len(tasks))
	for _, task := range tasks {
		r := processDecryptFile(task, outputDir, lrcPattern, withLyrics, qqMusicOptions)
		results = append(results, r)
		printProgress(r)
	}
	printSummary(results, false)
}

// processDecryptFile decrypts one file, optionally embeds lyrics, and writes the result.
func processDecryptFile(task fileTask, outputDir, lrcPattern string, withLyrics bool, qqMusicOptions decrypt.QQMusicOptions) taskResult {
	r := taskResult{src: task.srcPath}

	data, err := os.ReadFile(task.srcPath)
	if err != nil {
		r.decryptErr = fmt.Errorf("read: %w", err)
		return r
	}

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(task.srcPath), "."))
	audio, outExt, err := decryptFile(data, ext, qqMusicOptions)
	if err != nil {
		r.decryptErr = err
		return r
	}

	baseName := strings.TrimSuffix(filepath.Base(task.srcPath), filepath.Ext(task.srcPath))
	if withLyrics {
		lrcPath, err := findLyrics(filepath.Dir(task.srcPath), baseName, lrcPattern)
		if err != nil {
			r.lrcErr = err
		} else if lrcPath != "" {
			r.lrcSrc = lrcPath
			audio = embedLyricsInto(audio, outExt, lrcPath, &r.lrcErr)
		}
	}

	outPath := buildOutputPath(task.srcPath, task.inputDir, outputDir, baseName, outExt)
	r.dst = outPath

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		r.writeErr = fmt.Errorf("mkdir: %w", err)
		return r
	}
	if err := os.WriteFile(outPath, audio, 0644); err != nil {
		r.writeErr = fmt.Errorf("write: %w", err)
	}
	return r
}

func runEmbedMode(inputPath, outputDir, lrcPattern string) {
	tasks, err := collectTasks(inputPath, plainAudioExts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	if len(tasks) == 0 {
		fmt.Println("No MP3, FLAC, or OGG files found.")
		return
	}

	if outputDir != "" {
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			fmt.Fprintln(os.Stderr, "Error creating output directory:", err)
			os.Exit(1)
		}
	}

	results := make([]taskResult, 0, len(tasks))
	for _, task := range tasks {
		r := processEmbedFile(task, outputDir, lrcPattern)
		results = append(results, r)
		printProgress(r)
	}
	printSummary(results, true)
}

// processEmbedFile embeds lyrics into a plain (already-decoded) audio file.
// If no matching lyrics file is found, the result is marked as skipped.
func processEmbedFile(task fileTask, outputDir, lrcPattern string) taskResult {
	r := taskResult{src: task.srcPath}

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(task.srcPath), "."))
	baseName := strings.TrimSuffix(filepath.Base(task.srcPath), filepath.Ext(task.srcPath))

	lrcPath, err := findLyrics(filepath.Dir(task.srcPath), baseName, lrcPattern)
	if err != nil {
		r.lrcErr = err
		return r
	}
	if lrcPath == "" {
		r.skipped = true
		return r
	}
	r.lrcSrc = lrcPath

	data, err := os.ReadFile(task.srcPath)
	if err != nil {
		r.decryptErr = fmt.Errorf("read: %w", err)
		return r
	}

	audio := embedLyricsInto(data, ext, lrcPath, &r.lrcErr)
	if r.lrcErr != nil {
		return r
	}

	outPath := buildOutputPath(task.srcPath, task.inputDir, outputDir, baseName, ext)
	r.dst = outPath

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		r.writeErr = fmt.Errorf("mkdir: %w", err)
		return r
	}
	if err := os.WriteFile(outPath, audio, 0644); err != nil {
		r.writeErr = fmt.Errorf("write: %w", err)
	}
	return r
}

// embedLyricsInto reads an .lrc file, decodes it to text, and embeds it
// into audio. On any error, errOut is set and the original audio is returned.
func embedLyricsInto(audio []byte, ext, lrcPath string, errOut *error) []byte {
	lrcData, err := os.ReadFile(lrcPath)
	if err != nil {
		*errOut = fmt.Errorf("read lyrics: %w", err)
		return audio
	}
	lrcText, err := lyricsToUTF8(lrcData)
	if err != nil {
		*errOut = fmt.Errorf("decode lyrics: %w", err)
		return audio
	}
	embedded, err := decrypt.EmbedLyrics(audio, ext, lrcText)
	if err != nil {
		*errOut = fmt.Errorf("embed lyrics: %w", err)
		return audio
	}
	return embedded
}
