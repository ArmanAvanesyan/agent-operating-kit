package toolchain

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	syntheticChild()
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version":
			fmt.Print("synthetic-version")
			os.Exit(0)
		case "sleep":
			time.Sleep(time.Hour)
		case "overflow":
			io.WriteString(os.Stdout, strings.Repeat("x", 1<<20))
			os.Exit(0)
		case "environment":
			for _, entry := range os.Environ() {
				fmt.Println(entry)
			}
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}
func testPin(t *testing.T) a.ToolPin {
	p, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	p, e = filepath.EvalSymlinks(p)
	if e != nil {
		t.Fatal(e)
	}
	hash, e := HashFile(context.Background(), p, MaxBinaryBytes)
	if e != nil {
		t.Fatal(e)
	}
	return a.ToolPin{ID: "gh", Path: p, SHA256: hash, VersionOutput: "synthetic-version", Platform: Platform(), Extensions: []a.Extension{}}
}
func TestBoundedToolPinEnvironmentAndCleanup(t *testing.T) {
	p := testPin(t)
	ctx := context.Background()
	o, e := Observe(ctx, p, time.Now())
	if e != nil || !o.Verified || o.LiveQualification != "pending" {
		t.Fatalf("%+v %v", o, e)
	}
	out, e := Run(ctx, p, []string{"environment"}, nil, []string{}, "", 1024)
	if e != nil || bytes.Contains(out, []byte("PRIVATE_CANARY")) {
		t.Fatal("inherited environment")
	}
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, e = Run(ctx, p, []string{"sleep"}, nil, []string{}, "", 1024); e == nil || time.Since(start) > 3*time.Second {
		t.Fatal("unbounded cancellation")
	}
	if _, e = Run(context.Background(), p, []string{"overflow"}, nil, []string{}, "", 1024); e == nil {
		t.Fatal("overflow accepted")
	}
}
func TestToolIdentityAndUnsafePaths(t *testing.T) {
	p := testPin(t)
	p.SHA256 = strings.Repeat("0", 64)
	if Verify(context.Background(), p) == nil {
		t.Fatal("tamper")
	}
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(dir, "alias")
	if e = os.Symlink(p.Path, link); e == nil {
		if _, e = HashFile(context.Background(), link, MaxBinaryBytes); e == nil {
			t.Fatal("symlink")
		}
	}
	p.Path = "relative"
	if Verify(context.Background(), p) == nil {
		t.Fatal("relative tool")
	}
}
func TestArchiveIntegrityAndAllMemberBounds(t *testing.T) {
	for _, bad := range []string{"good", "traversal", "duplicate-dir", "directories-limit", "symlink", "missing", "tamper"} {
		t.Run(bad, func(t *testing.T) {
			var buf bytes.Buffer
			z := zip.NewWriter(&buf)
			target := "bin/gh"
			h := &zip.FileHeader{Name: target, Method: zip.Store}
			h.SetMode(0700)
			if bad == "traversal" {
				h.Name = "../gh"
			}
			if bad == "symlink" {
				h.SetMode(os.ModeSymlink | 0700)
			}
			if bad == "missing" {
				h.Name = "other"
			}
			w, _ := z.CreateHeader(h)
			w.Write([]byte("synthetic"))
			if bad == "duplicate-dir" {
				z.Create("dir/")
				z.Create("dir/")
			}
			if bad == "directories-limit" {
				for i := 0; i < 4097; i++ {
					z.Create(fmt.Sprintf("dir-%d/", i))
				}
			}
			z.Close()
			raw := buf.Bytes()
			a := a.Artifact{Format: "zip", SHA256: engine.Hash(raw), ExecutableSHA256: engine.Hash([]byte("synthetic")), Executable: target, MaxBytes: 1 << 20}
			if bad == "tamper" {
				raw = append(raw, 'x')
			}
			out, e := Extract(raw, a)
			if bad == "good" {
				if e != nil || string(out) != "synthetic" {
					t.Fatal(e)
				}
			} else if e == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}
