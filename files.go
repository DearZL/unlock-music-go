package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

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
func findLyrics(dir, baseName, lrcPattern string) (string, error) {
	pattern := strings.ReplaceAll(lrcPattern, "{name}", regexp.QuoteMeta(baseName))
	re, err := regexp.Compile("(?i)^" + pattern + "$")
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	exactName := baseName + ".lrc"
	var matches []string
	for _, e := range entries {
		if !e.IsDir() && re.MatchString(e.Name()) {
			if strings.EqualFold(e.Name(), exactName) {
				return filepath.Join(dir, e.Name()), nil
			}
			matches = append(matches, e.Name())
		}
	}
	if len(matches) == 0 {
		return "", nil
	}
	sort.Strings(matches)
	if len(matches) > 1 {
		return "", fmt.Errorf("multiple matching lyrics files: %s", strings.Join(matches, ", "))
	}
	return filepath.Join(dir, matches[0]), nil
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
