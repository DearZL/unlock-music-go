package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"unlock-music-go/decrypt"
)

// encryptedExts is the set of file extensions this tool can decrypt.
var encryptedExts = map[string]bool{
	"ncm": true, "uc": true,
	"mgg": true, "mgg0": true, "mggl": true, "mgg1": true,
	"mflac": true, "mflac0": true,
	"qmcflac": true, "qmcogg": true,
	"qmc0": true, "qmc2": true, "qmc3": true, "qmc4": true, "qmc6": true, "qmc8": true,
	"bkcmp3": true, "bkcm4a": true, "bkcflac": true, "bkcwav": true,
	"bkcape": true, "bkcogg": true, "bkcwma": true,
	"tkm": true, "666c6163": true, "6d7033": true, "6f6767": true, "6d3461": true, "776176": true,
	"cache": true,
	"tm2":   true, "tm6": true,
	"kwm": true,
	"xm":  true,
	"kgm": true, "kgma": true, "vpr": true,
	"x2m": true, "x3m": true,
	"mg3d": true,
}

// plainAudioExts is the set of plain (non-encrypted) audio formats that
// support lyrics embedding (-embed-lyrics mode).
var plainAudioExts = map[string]bool{
	"mp3": true, "flac": true, "ogg": true,
}

// fileTask describes one file to process.
type fileTask struct {
	srcPath  string // absolute path to the source file
	inputDir string // the root input path (for mirroring subdirectory structure)
}

// taskResult holds the outcome of processing one file.
type taskResult struct {
	src        string
	dst        string
	lrcSrc     string // lyrics file used, empty if none
	decryptErr error
	lrcErr     error // non-nil only when a lyrics file was found but embedding failed
	writeErr   error
	skipped    bool // embed-lyrics mode: no matching .lrc found, file was skipped
}

func usage() {
	fmt.Fprint(os.Stderr, `unlock-music-go —— 批量解密音乐文件并写入歌词

用法
  解密模式（默认）：
    unlock-music-go -i <文件或目录> [-o <输出目录>] [-lrc-pattern <正则>]

  写入歌词模式：
    unlock-music-go -i <文件或目录> -embed-lyrics [-o <输出目录>] [-lrc-pattern <正则>]

  查看标签模式：
    unlock-music-go -i <file.mp3|flac> -dump-tags

模式说明
  （默认）        解密加密音乐文件。解密后会自动在同目录查找对应 .lrc 并写入。

  -embed-lyrics  给已有 MP3/FLAC 文件写入歌词，不执行解密。
                 适用于已经是明文音频但希望补充歌词标签的场景。
                 未匹配到歌词文件会自动跳过。

  -dump-tags     输出 MP3（USLT）或 FLAC（LYRICS）中的歌词内容并退出。

参数
`)
	flag.PrintDefaults()

	fmt.Fprint(os.Stderr, `
歌词匹配规则
  -lrc-pattern 是一个正则模板，其中 {name} 会被替换为歌曲名（已转义）。
  匹配为大小写不敏感，并默认完全匹配。

    默认：{name}\.lrc
    示例：{name}[ ._-]*\.lrc
    示例：{name}.*\.lrc

  若未找到歌词文件，不报错，继续处理。

输出
  不使用 -o 时：输出到源文件同目录
  使用 -o 时：保持目录结构输出到指定目录
  在 -embed-lyrics 模式下且未指定 -o：会直接覆盖原文件

示例
  unlock-music-go -i song.mflac
  unlock-music-go -i ./Music -o ./output
  unlock-music-go -i ./Music -embed-lyrics
  unlock-music-go -i ./Music -embed-lyrics -o ./output -lrc-pattern "{name}.*\.lrc"
  unlock-music-go -i song.mp3 -dump-tags
`)
}

func main() {
	inputPath := flag.String("i", "", "Input file or directory (required)")
	outputDir := flag.String("o", "", "Output directory (default: same as each source file)")
	lrcPattern := flag.String("lrc-pattern", `{name}\.lrc`, "Regex template for lyrics detection; {name} = song base name")
	dumpTags := flag.Bool("dump-tags", false, "Print embedded lyrics from a decoded MP3/FLAC file, then exit")
	embedLyrics := flag.Bool("embed-lyrics", false, "Embed lyrics into plain (already-decoded) MP3/FLAC files")
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
		runEmbedMode(*inputPath, *outputDir, *lrcPattern)
		return
	}

	// ── decrypt mode (default) ──────────────────────────────────────────────
	runDecryptMode(*inputPath, *outputDir, *lrcPattern)
}

// ── decrypt mode ────────────────────────────────────────────────────────────

func runDecryptMode(inputPath, outputDir, lrcPattern string) {
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
		r := processDecryptFile(task, outputDir, lrcPattern)
		results = append(results, r)
		printProgress(r)
	}
	printSummary(results, false)
}

// processDecryptFile decrypts one file, optionally embeds lyrics, and writes the result.
func processDecryptFile(task fileTask, outputDir, lrcPattern string) taskResult {
	r := taskResult{src: task.srcPath}

	data, err := os.ReadFile(task.srcPath)
	if err != nil {
		r.decryptErr = fmt.Errorf("read: %w", err)
		return r
	}

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(task.srcPath), "."))
	audio, outExt, err := decryptFile(data, ext)
	if err != nil {
		r.decryptErr = err
		return r
	}

	baseName := strings.TrimSuffix(filepath.Base(task.srcPath), filepath.Ext(task.srcPath))
	if lrcPath := findLyrics(filepath.Dir(task.srcPath), baseName, lrcPattern); lrcPath != "" {
		r.lrcSrc = lrcPath
		audio = embedLyricsInto(audio, outExt, lrcPath, &r.lrcErr)
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

// ── embed-lyrics mode ────────────────────────────────────────────────────────

func runEmbedMode(inputPath, outputDir, lrcPattern string) {
	tasks, err := collectTasks(inputPath, plainAudioExts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	if len(tasks) == 0 {
		fmt.Println("No MP3 or FLAC files found.")
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

	lrcPath := findLyrics(filepath.Dir(task.srcPath), baseName, lrcPattern)
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

// ── shared helpers ───────────────────────────────────────────────────────────

// embedLyricsInto reads an .lrc file, converts it to UTF-8, and embeds it
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

// collectTasks walks inputPath (file or directory) and returns a task for
// every file whose extension is in the given exts set.
func collectTasks(inputPath string, exts map[string]bool) ([]fileTask, error) {
	info, err := os.Stat(inputPath)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(inputPath), "."))
		if !exts[ext] {
			supported := extList(exts)
			return nil, fmt.Errorf("%q has unsupported extension (expected one of: %s)", inputPath, supported)
		}
		abs, _ := filepath.Abs(inputPath)
		return []fileTask{{srcPath: abs, inputDir: filepath.Dir(abs)}}, nil
	}

	abs, _ := filepath.Abs(inputPath)
	var tasks []fileTask
	filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
		if exts[ext] {
			tasks = append(tasks, fileTask{srcPath: path, inputDir: abs})
		}
		return nil
	})
	return tasks, nil
}

// findLyrics looks for a lyrics file in dir matching the lrcPattern template.
// Returns the full path of the first match, or "" if none found.
func findLyrics(dir, baseName, lrcPattern string) string {
	pattern := strings.ReplaceAll(lrcPattern, "{name}", regexp.QuoteMeta(baseName))
	re, err := regexp.Compile("(?i)^" + pattern + "$")
	if err != nil {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() && re.MatchString(e.Name()) {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

// buildOutputPath computes the destination path for a processed file.
func buildOutputPath(srcPath, inputDir, outputDir, baseName, outExt string) string {
	outName := baseName + "." + outExt
	if outputDir == "" {
		return filepath.Join(filepath.Dir(srcPath), outName)
	}
	rel, err := filepath.Rel(inputDir, filepath.Dir(srcPath))
	if err != nil || rel == "." {
		return filepath.Join(outputDir, outName)
	}
	return filepath.Join(outputDir, rel, outName)
}

// extList returns a comma-separated list of keys from an extension map.
func extList(exts map[string]bool) string {
	keys := make([]string, 0, len(exts))
	for k := range exts {
		keys = append(keys, k)
	}
	return strings.Join(keys, ", ")
}

// ── progress & summary ───────────────────────────────────────────────────────

func printProgress(r taskResult) {
	if r.skipped {
		return // silently skip files with no matching lyrics in embed mode
	}
	src := filepath.Base(r.src)
	switch {
	case r.decryptErr != nil:
		fmt.Printf("  FAIL  %s\n        └─ %v\n", src, r.decryptErr)
	case r.writeErr != nil:
		fmt.Printf("  FAIL  %s\n        └─ %v\n", src, r.writeErr)
	case r.lrcErr != nil:
		fmt.Printf("  WARN  %s  →  %s\n        └─ lyrics: %v\n", src, filepath.Base(r.dst), r.lrcErr)
	case r.lrcSrc != "":
		fmt.Printf("  OK    %s  →  %s  [+lrc]\n", src, filepath.Base(r.dst))
	default:
		fmt.Printf("  OK    %s  →  %s\n", src, filepath.Base(r.dst))
	}
}

func printSummary(results []taskResult, embedMode bool) {
	total := len(results)
	ok, withLrc, failed, skipped := 0, 0, 0, 0
	var failedPaths []string

	for _, r := range results {
		switch {
		case r.skipped:
			skipped++
		case r.decryptErr != nil || r.writeErr != nil:
			failed++
			failedPaths = append(failedPaths, filepath.Base(r.src))
		default:
			ok++
			if r.lrcSrc != "" && r.lrcErr == nil {
				withLrc++
			}
		}
	}

	fmt.Println()
	fmt.Printf("━━━ Summary ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	if embedMode {
		fmt.Printf("  Scanned : %d\n", total)
	} else {
		fmt.Printf("  Total   : %d\n", total)
	}
	fmt.Printf("  Success : %d", ok)
	if withLrc > 0 {
		fmt.Printf("  (lyrics embedded: %d)", withLrc)
	}
	fmt.Println()
	if embedMode && skipped > 0 {
		fmt.Printf("  Skipped : %d  (no matching lyrics file)\n", skipped)
	}
	if failed > 0 {
		fmt.Printf("  Failed  : %d\n", failed)
		for _, p := range failedPaths {
			fmt.Printf("    • %s\n", p)
		}
	}
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
}

// ── decrypt dispatcher ───────────────────────────────────────────────────────

func decryptFile(data []byte, ext string) ([]byte, string, error) {
	switch ext {
	case "ncm":
		r, err := decrypt.DecryptNcm(data)
		if err != nil {
			return nil, "", err
		}
		return r.Audio, r.Ext, nil

	case "uc":
		audio := decrypt.DecryptNcmCache(data)
		return audio, decrypt.SniffAudioExt(audio), nil

	case "cache":
		audio := decrypt.DecryptQmcCache(data)
		return audio, decrypt.SniffAudioExt(audio), nil

	case "mgg", "mgg0", "mggl", "mgg1",
		"mflac", "mflac0",
		"qmcflac", "qmcogg",
		"qmc0", "qmc2", "qmc3", "qmc4", "qmc6", "qmc8",
		"bkcmp3", "bkcm4a", "bkcflac", "bkcwav", "bkcape", "bkcogg", "bkcwma",
		"tkm", "666c6163", "6d7033", "6f6767", "6d3461", "776176":
		r, err := decrypt.DecryptQmc(data, ext)
		if err != nil {
			return nil, "", err
		}
		return r.Audio, r.Ext, nil

	case "tm2", "tm6":
		r, err := decrypt.DecryptTm(data)
		if err != nil {
			return nil, "", err
		}
		return r.Audio, r.Ext, nil

	case "kwm":
		r, err := decrypt.DecryptKwm(data)
		if err != nil {
			return nil, "", err
		}
		return r.Audio, r.Ext, nil

	case "xm":
		r, err := decrypt.DecryptXm(data)
		if err != nil {
			return nil, "", err
		}
		return r.Audio, r.Ext, nil

	case "kgm", "kgma":
		r, err := decrypt.DecryptKgm(data, false)
		if err != nil {
			return nil, "", err
		}
		return r.Audio, r.Ext, nil

	case "vpr":
		r, err := decrypt.DecryptKgm(data, true)
		if err != nil {
			return nil, "", err
		}
		return r.Audio, r.Ext, nil

	case "x2m":
		r, err := decrypt.DecryptX2M(data)
		if err != nil {
			return nil, "", err
		}
		return r.Audio, r.Ext, nil

	case "x3m":
		r, err := decrypt.DecryptX3M(data)
		if err != nil {
			return nil, "", err
		}
		return r.Audio, r.Ext, nil

	case "mg3d":
		r, err := decrypt.DecryptMg3d(data)
		if err != nil {
			return nil, "", err
		}
		return r.Audio, "wav", nil

	default:
		return nil, "", fmt.Errorf("unsupported extension: .%s", ext)
	}
}
