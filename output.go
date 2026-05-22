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

func printSummary(results []taskResult, embedMode bool) {
	total := len(results)
	ok, withLrc, failed, skipped := 0, 0, 0, 0
	var failedPaths []string

	for _, r := range results {
		switch {
		case r.skipped:
			skipped++
		case r.decryptErr != nil || r.writeErr != nil || (embedMode && r.lrcErr != nil):
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
