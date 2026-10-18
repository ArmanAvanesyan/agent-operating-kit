package render

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Canonical guide references must stay within the declared, bundled skill tree.
func validateSkillGuides(packDir, root string, skills []map[string]any) error {
	for _, skill := range skills {
		slug := regexp.MustCompile(`[^a-zA-Z0-9_-]+`).ReplaceAllString(strings.ToLower(orString(skill["id"], orString(skill["name"], "skill"))), "-")
		guide := orString(skill["sourcePath"], root+"/"+slug+"/SKILL.md")
		if !regexp.MustCompile(`^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)*$`).MatchString(guide) || !strings.HasPrefix(guide, root+"/") {
			return fmt.Errorf("unsafe canonical skill guide: %s", guide)
		}
		for _, part := range strings.Split(guide, "/") {
			if part == "." || part == ".." {
				return fmt.Errorf("unsafe canonical skill guide: %s", guide)
			}
		}
		path := filepath.Join(packDir, filepath.FromSlash(guide))
		if err := rejectSymlinkComponents(packDir, path); err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("missing or unsafe canonical skill guide: %s", guide)
		}
	}
	return nil
}
