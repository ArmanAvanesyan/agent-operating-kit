//go:build !windows

package hostbridge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNativeRunnerStopKillsDescendants(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "descendant-ran")
	l := helperLaunch(t, "tree")
	l.Env = append(l.Env, "AOK_MARKER="+marker)
	p, err := (NativeRunner{}).Start(context.Background(), l)
	if err != nil {
		t.Fatal(err)
	}
	c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err = p.Receive(c); err != nil {
		t.Fatal(err)
	}
	_ = p.Stop()
	_ = p.Wait()
	time.Sleep(700 * time.Millisecond)
	if _, err = os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("descendant escaped process group (marker stat: %v)", err)
	}
}
