package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

// CanonicalTempRoot returns the platform temporary directory with symlinked
// path components resolved. Strict path tests require a real directory path:
// macOS exposes /tmp as a symlink to /private/tmp, while Linux commonly uses
// /tmp directly.
func CanonicalTempRoot(t testing.TB) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatalf("resolve temporary directory: %v", err)
	}
	return root
}
