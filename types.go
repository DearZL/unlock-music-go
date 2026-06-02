package main

// encryptedExts is the set of file extensions this tool can decrypt.
var encryptedExts = map[string]bool{
	"ncm": true, "uc": true,
	"mgg": true, "mgg0": true, "mggl": true, "mgg1": true,
	"mflac": true, "mflac0": true,
	"qmcflac": true, "qmcogg": true,
	"qmc0": true, "qmc2": true, "qmc3": true, "qmc4": true, "qmc6": true, "qmc8": true,
	"bkcmp3": true, "bkcm4a": true, "mmp4": true, "bkcflac": true, "bkcwav": true,
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
