// Package task implements aok task sync|packet via the gh CLI.
package task

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hooks"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/kit"
)

var (
	issueRE = regexp.MustCompile(`github\.com/(?P<owner>[^/]+)/(?P<repo>[^/]+)/issues/(?P<num>\d+)`)
	prRE    = regexp.MustCompile(`github\.com/(?P<owner>[^/]+)/(?P<repo>[^/]+)/pull/(?P<num>\d+)`)
)

// GhRunner runs gh commands. Tests replace this with a fake.
var GhRunner = defaultGhRunner

func defaultGhRunner(args []string) ([]byte, error) {
	cmd := exec.Command("gh", args...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("%s: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}
	return out, nil
}

// ParsedRef is a GitHub issue or PR reference.
type ParsedRef struct {
	Kind  string // "issue" or "pr"
	Owner string
	Repo  string
	Num   string
}

// ParseGitHubRef extracts owner/repo/number from an issue or PR URL.
func ParseGitHubRef(ref string) *ParsedRef {
	if m := issueRE.FindStringSubmatch(ref); m != nil {
		return &ParsedRef{Kind: "issue", Owner: m[1], Repo: m[2], Num: m[3]}
	}
	if m := prRE.FindStringSubmatch(ref); m != nil {
		return &ParsedRef{Kind: "pr", Owner: m[1], Repo: m[2], Num: m[3]}
	}
	return nil
}

// View runs gh issue/pr view and returns enriched JSON data.
func View(ref string) (map[string]any, error) {
	parsed := ParseGitHubRef(ref)
	if parsed == nil {
		return nil, kit.NewError("Only GitHub issue and PR URLs are supported by this local CLI implementation.")
	}
	fullRepo := parsed.Owner + "/" + parsed.Repo
	var args []string
	if parsed.Kind == "issue" {
		args = []string{
			"issue", "view", parsed.Num,
			"--repo", fullRepo,
			"--comments",
			"--json", "title,body,labels,state,comments,assignees,milestone,projectItems,url",
		}
	} else {
		args = []string{
			"pr", "view", parsed.Num,
			"--repo", fullRepo,
			"--comments",
			"--json", "title,body,state,headRefName,baseRefName,files,commits,checks,reviews,comments,url",
		}
	}
	out, err := GhRunner(args)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := json.Unmarshal(out, &data); err != nil {
		return nil, err
	}
	data["_kind"] = parsed.Kind
	data["_repo"] = fullRepo
	data["_number"] = parsed.Num
	return data, nil
}

// Sync prints gh JSON for a ref to stdout.
func Sync(ref string, stdout io.Writer) (int, error) {
	data, err := View(ref)
	if err != nil {
		return 1, err
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		return 1, err
	}
	return 0, nil
}

// PacketMarkdown builds the task packet markdown from gh view data.
func PacketMarkdown(data map[string]any) string {
	body, _ := data["body"].(string)
	title, _ := data["title"].(string)
	problem := hooks.ExtractSection(body, "Problem")
	if problem == "" {
		problem = title
	}
	scope := hooks.ExtractSection(body, "Scope")
	criteria := hooks.ExtractSection(body, "Acceptance Criteria")
	targetRepo := hooks.ExtractSection(body, "Target Repo")
	if targetRepo == "" {
		if r, ok := data["_repo"].(string); ok {
			targetRepo = r
		}
	}
	verification := hooks.ExtractSection(body, "Verification")
	risks := hooks.ExtractSection(body, "Risks / Blockers")
	url, _ := data["url"].(string)
	kind, _ := data["_kind"].(string)
	issue, pr := "", ""
	if kind == "issue" {
		issue = url
	} else if kind == "pr" {
		pr = url
	}
	if scope == "" {
		scope = "In scope:\n-\n\nOut of scope:\n-"
	}
	if criteria == "" {
		criteria = "- [ ]"
	}
	if risks == "" {
		risks = "-"
	}
	if verification == "" {
		verification = "Commands or evidence required:\n-"
	}

	var b strings.Builder
	b.WriteString("# Agent Task Packet\n\n")
	b.WriteString("## Metadata\n\n")
	b.WriteString("| Field | Value |\n|---|---|\n")
	b.WriteString("| Project Item | |\n")
	fmt.Fprintf(&b, "| Issue | %s |\n", issue)
	fmt.Fprintf(&b, "| Pull Request | %s |\n", pr)
	fmt.Fprintf(&b, "| Target Repo | %s |\n", targetRepo)
	b.WriteString("| Branch | |\n| Status | |\n| Phase | |\n| Priority | |\n")
	b.WriteString("| Attempt | 0 |\n| Primary Agent | |\n| QA Agent | |\n\n")
	b.WriteString("## Problem\n\n")
	b.WriteString(problem)
	b.WriteString("\n\n## Scope\n\n")
	b.WriteString(scope)
	b.WriteString("\n\n## Acceptance Criteria\n\n")
	b.WriteString(criteria)
	b.WriteString("\n\n## Context\n\n")
	b.WriteString("Relevant files:\n-\n\nRelevant docs:\n-\n\nDependencies:\n-\n\n")
	b.WriteString("Risks / Blockers:\n")
	b.WriteString(risks)
	b.WriteString("\n\n## Pre-Hook Results\n\n")
	b.WriteString("- `pre_project_sync`:\n")
	b.WriteString("- `pre_scope_check`:\n")
	b.WriteString("- `pre_agent_select`:\n")
	b.WriteString("- `pre_worktree`:\n")
	b.WriteString("- `pre_risk_check`:\n\n")
	b.WriteString("## Implementation Notes\n\n")
	b.WriteString("## Verification Plan\n\n")
	b.WriteString(verification)
	b.WriteString("\n\n## QA Verdict\n\n")
	b.WriteString("Verdict: `PENDING`\n\n")
	b.WriteString("Evidence:\n-\n\nFailures:\n-\n\n")
	b.WriteString("## GitHub Update Log\n\n-\n\n")
	b.WriteString("## Final Summary\n")
	return b.String()
}

// Packet writes or prints a task packet for ref. Returns exit code.
func Packet(ref, outputPath string, stdout io.Writer) (int, error) {
	data, err := View(ref)
	if err != nil {
		return 1, err
	}
	packet := PacketMarkdown(data)
	if outputPath != "" {
		dir := filepath.Dir(outputPath)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return 1, err
			}
		}
		if err := os.WriteFile(outputPath, []byte(packet), 0o644); err != nil {
			return 1, err
		}
		fmt.Fprintf(stdout, "Wrote %s\n", outputPath)
		return 0, nil
	}
	fmt.Fprint(stdout, packet)
	return 0, nil
}
