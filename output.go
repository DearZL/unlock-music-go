package main

import (
	"fmt"
	"path/filepath"
)

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
		dst := filepath.Base(r.dst)
		if r.dst == "" {
			dst = "(not written)"
		}
		fmt.Printf("  WARN  %s  →  %s\n        └─ lyrics: %v\n", src, dst, r.lrcErr)
	case r.lrcSrc != "":
		fmt.Printf("  OK    %s  →  %s  [+lrc]\n", src, filepath.Base(r.dst))
	default:
		fmt.Printf("  OK    %s  →  %s\n", src, filepath.Base(r.dst))
	}
}

// printSummary renders aggregate task status and reports whether every
// processed task completed successfully. The caller maps a false result to a
// non-zero process status, while still printing all per-file failures.
func printSummary(results []taskResult, embedMode, lyricsEnabled bool) bool {
	summary := summarizeResults(results, embedMode)

	fmt.Println()
	fmt.Printf("━━━ Summary ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	if embedMode {
		fmt.Printf("  Scanned : %d\n", summary.total)
	} else {
		fmt.Printf("  Total   : %d\n", summary.total)
	}
	fmt.Printf("  Success : %d\n", summary.success)
	if lyricsEnabled && !embedMode {
		fmt.Printf("  Lyrics  : %d embedded, %d missing", summary.lyricsEmbedded, summary.lyricsMissing)
		if summary.lyricsWarnings > 0 {
			fmt.Printf(", %d warning", summary.lyricsWarnings)
		}
		fmt.Println()
	}
	if embedMode && summary.skipped > 0 {
		fmt.Printf("  Skipped : %d  (no matching lyrics file)\n", summary.skipped)
	}
	if summary.failed > 0 {
		fmt.Printf("  Failed  : %d\n", summary.failed)
		for _, p := range summary.failedPaths {
			fmt.Printf("    • %s\n", p)
		}
	}
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	return summary.failed == 0
}

type taskSummary struct {
	total, success, failed, skipped int
	lyricsEmbedded, lyricsMissing   int
	lyricsWarnings                  int
	failedPaths                     []string
}

func summarizeResults(results []taskResult, embedMode bool) taskSummary {
	summary := taskSummary{total: len(results)}
	for _, r := range results {
		switch {
		case r.skipped:
			summary.skipped++
		case taskFailed(r, embedMode):
			summary.failed++
			summary.failedPaths = append(summary.failedPaths, filepath.Base(r.src))
		default:
			summary.success++
			if r.lrcSrc != "" && r.lrcErr == nil {
				summary.lyricsEmbedded++
			}
			if r.lrcMissing {
				summary.lyricsMissing++
			}
			if r.lrcErr != nil {
				summary.lyricsWarnings++
			}
		}
	}
	return summary
}

func taskFailed(r taskResult, embedMode bool) bool {
	return r.decryptErr != nil || r.writeErr != nil || (embedMode && r.lrcErr != nil)
}
