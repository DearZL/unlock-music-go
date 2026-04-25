[🇨🇳 中文](./README.zh.md) | 🇺🇸 English
# unlock-music-go

A command-line tool written in Go that decrypts encrypted music files from
Chinese streaming platforms (NetEase, QQ Music, Kugou, Kuwo, Ximalaya, etc.)
and optionally embeds LRC lyrics into the decoded audio tags.

---

## Features

- Decrypt 30+ encrypted file formats from all major Chinese streaming platforms
- Auto-detect and embed matching `.lrc` lyrics files (MP3 → ID3v2 USLT; FLAC → Vorbis Comment)
- Batch mode: process an entire directory tree recursively
- Embed lyrics into plain (already-decoded) MP3/FLAC files without decryption
- Flexible regex-based lyrics matching with `{name}` placeholder
- GBK / UTF-16 LRC files are converted to UTF-8 automatically
- Skip-on-error: one bad file never aborts the whole batch
- Mirror subdirectory structure under the output directory

---

## Supported Encrypted Formats

| Platform | Extensions |
|---|---|
| **NetEase Cloud Music** | `.ncm` `.uc` (cache) |
| **QQ Music** | `.mgg` `.mgg0` `.mggl` `.mgg1` `.mflac` `.mflac0` `.qmcflac` `.qmcogg` `.qmc0` `.qmc2` `.qmc3` `.qmc4` `.qmc6` `.qmc8` `.bkcmp3` `.bkcm4a` `.bkcflac` `.bkcwav` `.bkcape` `.bkcogg` `.bkcwma` `.tkm` `.cache` `.666c6163` `.6d7033` `.6f6767` `.6d3461` `.776176` |
| **QQ Music (old)** | `.tm2` `.tm6` |
| **Kuwo Music** | `.kwm` |
| **Ximalaya** | `.x2m` `.x3m` `.xm` |
| **Kugou Music** | `.kgm` `.kgma` `.vpr` |
| **Migumusic** | `.mg3d` |

Decrypted output is standard MP3, FLAC, OGG, M4A, WAV, or APE depending on
what is contained inside the encrypted file.

### Lyrics embedding support

Lyrics can be embedded into **MP3** (ID3v2.3 `USLT` frame) and **FLAC** || **OGG**
(Vorbis Comment `LYRICS` field). Other output formats (OGG, M4A, WAV, APE)
are written without embedded lyrics even if a `.lrc` file is present.

---

## Installation

### Pre-built binary

Download `unlock.exe` (Windows) or the Linux binary from the releases page.

### Build from source

Requirements: **Go 1.21+**

```bash
git clone <repo-url>
cd unlock-music-go
go build -o unlock .
```

Cross-compile for Windows from Linux/macOS:

```bash
GOOS=windows GOARCH=amd64 go build -o unlock.exe .
```

---

## Usage

```
unlock-music-go -i <file_or_dir> [-o <output_dir>] [-lrc-pattern <regex>]
unlock-music-go -i <file_or_dir> -embed-lyrics [-o <output_dir>] [-lrc-pattern <regex>]
unlock-music-go -i <file.mp3|flac> -dump-tags
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `-i` | *(required)* | Input file or directory. Directories are searched recursively. |
| `-o` | *(same as source)* | Output directory. Source subdirectory structure is mirrored. |
| `-lrc-pattern` | `{name}\.lrc` | Regex template for lyrics file detection. `{name}` is replaced with the regex-escaped song name. Matching is case-insensitive. |
| `-embed-lyrics` | false | Switch to embed-lyrics mode (no decryption). |
| `-dump-tags` | false | Print the lyrics embedded in an MP3 or FLAC file, then exit. |

### Modes

#### Decrypt mode (default)

Decrypts every supported encrypted file found under `-i`. For each file, the
tool checks the same directory for a matching lyrics file using `-lrc-pattern`
and embeds it automatically if found.

```bash
# Decrypt a single file
unlock-music-go -i "周杰伦 - 最长的电影.mflac"

# Decrypt a whole library, write to ./output
unlock-music-go -i ~/Music -o ~/output

# Decrypt and match lyrics with a looser pattern
unlock-music-go -i ~/Music -o ~/output -lrc-pattern "{name}.*\.lrc"
```

#### Embed-lyrics mode (`-embed-lyrics`)

Embeds lyrics into plain (already-decoded) MP3 or FLAC files. No decryption
is performed. Files with no matching `.lrc` are silently skipped.

If `-o` is not specified the original file is **overwritten in place**.

```bash
# Embed lyrics into all MP3/FLAC files in a directory (in place)
unlock-music-go -i ~/Music -embed-lyrics

# Same, but write modified copies to ./output instead of overwriting
unlock-music-go -i ~/Music -embed-lyrics -o ~/output

# Custom lrc pattern (e.g. "Song - Artist.lrc")
unlock-music-go -i ~/Music -embed-lyrics -lrc-pattern "{name}[ ._-]*\.lrc"
```

#### Dump-tags mode (`-dump-tags`)

Reads and prints the lyrics text already embedded in an MP3 or FLAC file.
Useful for verifying that lyrics were written correctly.

```bash
unlock-music-go -i song.mp3 -dump-tags
unlock-music-go -i song.flac -dump-tags
```

### Output format

```
  OK    周杰伦 - 最长的电影.mflac  →  周杰伦 - 最长的电影.flac  [+lrc]
  OK    song.ncm                   →  song.mp3
  WARN  song2.mflac  →  song2.flac
        └─ lyrics: embed lyrics: lyrics embedding is not supported for .flac files
  FAIL  broken.qmc0
        └─ qmc: key derivation failed: ...

━━━ Summary ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Total   : 4
  Success : 3  (lyrics embedded: 1)
  Failed  : 1
    • broken.qmc0
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

---

## Lyrics file detection

The `-lrc-pattern` value is a Go regex where `{name}` expands to the
regex-escaped base name of the song file. The full filename (including
extension) must match the pattern (anchored `^...$`, case-insensitive).

| Pattern | Matches |
|---|---|
| `{name}\.lrc` *(default)* | `Song Title.lrc` |
| `{name}[ ._-]*\.lrc` | `Song Title.lrc`, `Song Title-.lrc`, `Song Title_.lrc` |
| `{name}.*\.lrc` | Any `.lrc` starting with the song name |

Lyrics files are read in UTF-8, UTF-8 BOM, UTF-16 LE/BE (with BOM), or GBK /
GB18030 (common for Chinese lyrics created on Windows) — all automatically
converted to UTF-8 before embedding.

---

## Project structure

```
unlock-music-go/
├── main.go          # CLI entry point, modes, task orchestration
├── encoding.go      # LRC encoding detection & UTF-8 conversion
├── go.mod
├── go.sum
└── decrypt/
    ├── detect.go    # Audio format sniffing (SniffAudioExt)
    ├── lyrics.go    # Embed lyrics into MP3 (ID3v2 USLT) / FLAC (Vorbis Comment)
    ├── tags_read.go # Read/dump embedded lyrics from MP3 / FLAC
    ├── ncm.go       # NetEase Cloud Music (.ncm)
    ├── ncmcache.go  # NetEase cache (.uc)
    ├── qmc.go       # QQ Music dispatcher (QTag / STag / legacy)
    ├── qmc_cipher.go# QMC stream ciphers (Static / Map / RC4)
    ├── qmc_key.go   # QMC V2 key derivation (Tencent TEA-CBC + base64)
    ├── qmccache.go  # QQ Music cache (.cache)
    ├── tea.go       # TEA block cipher (used by QMC key derivation)
    ├── tm.go        # QQ Music old format (.tm2 .tm6)
    ├── kwm.go       # Kuwo Music (.kwm)
    ├── xm.go        # Ximalaya legacy (.xm)
    ├── ximalaya.go  # Ximalaya (.x2m .x3m)
    ├── kgm.go       # Kugou Music (.kgm .kgma .vpr)
    └── mg3d.go      # Migumusic (.mg3d)
```

---

## Dependencies

| Package | Purpose |
|---|---|
| `golang.org/x/text` | GBK / GB18030 decoding for Chinese LRC files |

All decryption logic is implemented from scratch with no other external dependencies.

---

## License

This project is for personal and educational use only. Respect the terms of
service of the respective music platforms.
