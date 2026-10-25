//go:build !windows

package toolchain

import (
	"context"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func syntheticChild() {
	if len(os.Args) < 3 {
		return
	}
	switch os.Args[1] {
	case "nested-broker":
		pin := testSelfPin()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _ = RunNested(ctx, pin, []string{"helper", os.Args[2]}, nil, []string{}, "", 1024)
		os.Exit(0)

	case "helper":
		for {
			os.WriteFile(os.Args[2], []byte(time.Now().String()), 0600)
			time.Sleep(10 * time.Millisecond)
		}
	case "spawn":
		child := exec.Command(os.Args[0], "helper", os.Args[2])
		if child.Start() != nil {
			os.Exit(9)
		}
		for i := 0; i < 100; i++ {
			if _, e := os.Stat(os.Args[2]); e == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if len(os.Args) > 3 && os.Args[3] == "exit" {
			os.Exit(0)
		}
		time.Sleep(time.Hour)
	}
}
func TestDescendantCleanupOnDeadlineAndNormalExit(t *testing.T) {
	for _, mode := range []string{"deadline", "exit"} {
		t.Run(mode, func(t *testing.T) {
			dir, e := filepath.EvalSymlinks(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(dir, "heartbeat")
			p := testPin(t)
			limit := 2 * time.Second
			if mode == "exit" {
				limit = 10 * time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), limit)
			defer cancel()
			_, err := Run(ctx, p, []string{"spawn", path, mode}, nil, []string{}, dir, 1024)
			if mode == "exit" && err != nil {
				t.Fatal(err)
			}
			before, e := os.ReadFile(path)
			if e != nil {
				t.Fatal("helper never started")
			}
			time.Sleep(75 * time.Millisecond)
			after, e := os.ReadFile(path)
			if e != nil || string(before) != string(after) {
				t.Fatal("helper still executing after cleanup")
			}
		})
	}
}

func testSelfPin() a.ToolPin {
	path, _ := os.Executable()
	path, _ = filepath.EvalSymlinks(path)
	sha, _ := HashFile(context.Background(), path, MaxBinaryBytes)
	return a.ToolPin{ID: "workkit-keychain-helper", Path: path, SHA256: sha, VersionOutput: "fixture", Platform: Platform()}
}
func TestNestedBrokerHelperCancellation(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	heartbeat := filepath.Join(dir, "nested-heartbeat")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = Run(ctx, testSelfPin(), []string{"nested-broker", heartbeat}, nil, []string{}, dir, 1024)
	before, e := os.ReadFile(heartbeat)
	if e != nil {
		t.Fatal("nested helper never entered")
	}
	time.Sleep(75 * time.Millisecond)
	after, e := os.ReadFile(heartbeat)
	if e != nil || string(before) != string(after) {
		t.Fatal("nested helper survived outer completion")
	}
}
