// Package importhistory implements aok import history.
package importhistory

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hooks"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/kit"
)

const maxFileBytes = 500_000

var (
	ignoredParts = map[string]bool{
		".git": true, "node_modules": true, "dist": true,
		"build": true, ".venv": true, "__pycache__": true,
	}
	keywords = []string{
		"Agent Update",
		"Acceptance Criteria",
		"Verification",
		"QA",
		"Verdict:",
		"GitHub Update",
		"Final Summary",
		"Blocker",
		"Escalation",
	}
	titleRE       = regexp.MustCompile(`(?m)^#\s+(.+)$`)
	agentUpdateRE = regexp.MustCompile(`(?im)^##\s+Agent Update\s*$`)
)

// Summarize walks path for md/txt/log files and builds a keyword digest report.
func Summarize(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", kit.NewErrorf("Source is not a directory: %s", abs)
	}
	files, err := candidateHistoryFiles(abs)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("# Agent History Import\n\n")
	fmt.Fprintf(&b, "Source: %s\n\n", abs)
	matched := 0
	for _, filePath := range files {
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		text := string(data)
		lower := strings.ToLower(text)
		var hits []string
		for _, kw := range keywords {
			if strings.Contains(lower, strings.ToLower(kw)) {
				hits = append(hits, kw)
			}
		}
		if len(hits) == 0 {
			continue
		}
		matched++
		rel, err := filepath.Rel(abs, filePath)
		if err != nil {
			rel = filePath
		}
		title := filepath.Base(rel)
		if m := titleRE.FindStringSubmatch(text); len(m) > 1 {
			title = strings.TrimSpace(m[1])
		}
		fmt.Fprintf(&b, "## %s\n\n", rel)
		fmt.Fprintf(&b, "- Title: %s\n", title)
		fmt.Fprintf(&b, "- Signals: %s\n", strings.Join(hits, ", "))
		if agentUpdateRE.MatchString(text) {
			b.WriteString("- Contains agent update block\n")
		}
		if final := hooks.ExtractSection(text, "Final Summary"); final != "" {
			compact := strings.Join(strings.Fields(final), " ")
			if len(compact) > 240 {
				compact = compact[:240]
			}
			fmt.Fprintf(&b, "- Final summary excerpt: %s\n", compact)
		}
		b.WriteString("\n")
	}
	b.WriteString("## Result\n\n")
	fmt.Fprintf(&b, "- Scanned files: %d\n", len(files))
	fmt.Fprintf(&b, "- Matched files: %d\n", matched)
	b.WriteString("- Hidden chat/session memory was not used.\n")
	return b.String(), nil
}

// History writes the digest to stdout or --output path. Returns exit code.
func History(sourcePath, outputPath string, stdout io.Writer) (int, error) {
	report, err := Summarize(sourcePath)
	if err != nil {
		return 1, err
	}
	if outputPath != "" {
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			return 1, err
		}
		if err := os.WriteFile(outputPath, []byte(report), 0o644); err != nil {
			return 1, err
		}
		fmt.Fprintf(stdout, "Wrote %s\n", outputPath)
		return 0, nil
	}
	fmt.Fprint(stdout, report)
	return 0, nil
}

func candidateHistoryFiles(root string) ([]string, error) {
	var names []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		parts := strings.Split(rel, string(os.PathSeparator))
		for _, part := range parts {
			if ignoredParts[part] {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".md" && ext != ".txt" && ext != ".log" {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxFileBytes {
			return nil
		}
		names = append(names, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	return names, nil
}
