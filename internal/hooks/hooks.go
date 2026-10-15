// Package hooks implements aok hooks pre|after for task packet checks.
package hooks

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/kit"
)

var (
	headingRE    = regexp.MustCompile(`(?m)^##\s+`)
	targetRepoRE = regexp.MustCompile(`(?i)\|\s*Target Repo\s*\|\s*[^|\s][^|]*\|`)
	nonContentRE = regexp.MustCompile(`[-\[\]\s` + "`" + `:_|]`)
)

// ReadPacket loads a task packet file.
func ReadPacket(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", kit.NewErrorf("Task packet not found: %s", path)
		}
		return "", err
	}
	return string(data), nil
}

// ExtractSection returns the body under ## heading (case-insensitive), trimmed.
func ExtractSection(body, heading string) string {
	pattern := regexp.MustCompile(`(?im)^##\s+` + regexp.QuoteMeta(heading) + `\s*$`)
	match := pattern.FindStringIndex(body)
	if match == nil {
		return ""
	}
	start := match[1]
	rest := body[start:]
	next := headingRE.FindStringIndex(rest)
	end := len(body)
	if next != nil {
		end = start + next[0]
	}
	return strings.TrimSpace(body[start:end])
}

// SectionPresent reports whether ## heading exists.
func SectionPresent(text, heading string) bool {
	pattern := regexp.MustCompile(`(?m)^##\s+` + regexp.QuoteMeta(heading) + `\s*$`)
	return pattern.MatchString(text)
}

// SectionHasContent reports whether the section has non-placeholder content.
func SectionHasContent(text, heading string) bool {
	content := ExtractSection(text, heading)
	stripped := nonContentRE.ReplaceAllString(content, "")
	return stripped != ""
}

// Pre runs pre-hooks checks. Returns exit code 1 when checks fail.
func Pre(packetPath string, stdout io.Writer) (int, error) {
	text, err := ReadPacket(packetPath)
	if err != nil {
		return 1, err
	}
	checks := []struct {
		name string
		ok   bool
	}{
		{"Problem", SectionHasContent(text, "Problem")},
		{"Acceptance Criteria", SectionHasContent(text, "Acceptance Criteria")},
		{"Verification Plan", SectionHasContent(text, "Verification Plan")},
		{"Metadata", SectionPresent(text, "Metadata")},
		{"Target Repo", targetRepoRE.MatchString(text)},
	}
	var failed []string
	for _, c := range checks {
		if !c.ok {
			failed = append(failed, c.name)
		}
	}
	if len(failed) > 0 {
		fmt.Fprintln(stdout, "Pre-hooks failed. Missing or incomplete:")
		for _, name := range failed {
			fmt.Fprintf(stdout, "- %s\n", name)
		}
		return 1, nil
	}
	fmt.Fprintln(stdout, "Pre-hooks passed.")
	fmt.Fprintln(stdout, "- pre_project_sync: packet present")
	fmt.Fprintln(stdout, "- pre_scope_check: required scope fields present")
	fmt.Fprintln(stdout, "- pre_agent_select: role fields available")
	fmt.Fprintln(stdout, "- pre_worktree: run repo-local git checks before edits")
	fmt.Fprintln(stdout, "- pre_risk_check: inspect risks before implementation")
	return 0, nil
}

// After prints the Agent Update stub and optional warnings.
func After(packetPath string, stdout, stderr io.Writer) (int, error) {
	text, err := ReadPacket(packetPath)
	if err != nil {
		return 1, err
	}
	hasQA := !strings.Contains(text, "Verdict: `PENDING`") && strings.Contains(text, "Verdict:")
	hasSummary := SectionHasContent(text, "Final Summary") || SectionHasContent(text, "Implementation Notes")
	if !hasSummary {
		fmt.Fprintln(stdout, "After-hooks warning: final summary or implementation notes are still empty.")
	}
	fmt.Fprintln(stdout, "## Agent Update")
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Status:")
	fmt.Fprintln(stdout, "Agent:")
	fmt.Fprintln(stdout, "Summary:")
	fmt.Fprintln(stdout, "Verification:")
	fmt.Fprintln(stdout, "Evidence:")
	fmt.Fprintln(stdout, "Next:")
	fmt.Fprintln(stdout, "Blockers:")
	if !hasQA {
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Warning: QA verdict is still pending.")
	}
	return 0, nil
}
