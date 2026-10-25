package toolchain

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInstallOneSendSideBySideAndInterruptedPreservation(t *testing.T) {
	// Fixed official URL is routed only to this disposable TLS fixture. No public
	// network, credential, real executable or package manager is used.
	raw := []byte("synthetic-executable-no-runtime")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("download credential")
		}
		w.Write(raw)
	}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	original := http.DefaultTransport
	transport := original.(*http.Transport).Clone()
	transport.DialTLSContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "github.com:443" {
			return nil, errors.New("unexpected download host")
		}
		d := tls.Dialer{Config: &tls.Config{RootCAs: roots, ServerName: "example.com"}}
		return d.DialContext(ctx, network, server.Listener.Addr().String())
	}
	http.DefaultTransport = transport
	defer func() { http.DefaultTransport = original; transport.CloseIdleConnections() }()
	for _, mode := range []string{"install", "update", "drift", "expired-before-send", "expired-before-publish"} {
		t.Run(mode, func(t *testing.T) {
			dir, e := filepath.EvalSymlinks(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			old := filepath.Join(dir, "old")
			prior := []byte("operator-owned-old")
			os.WriteFile(old, prior, 0700)
			dest := filepath.Join(dir, "new")
			p := a.InstallPlan{Version: a.Version, Kind: "ToolInstallPlan", Action: "install", Destination: dest, Artifact: a.Artifact{ToolID: "gh", ToolVersion: "1.2.3", Platform: Platform(), URL: "https://github.com/cli/cli/releases/download/v1.2.3/fixture", SHA256: engine.Hash(raw), ExecutableSHA256: engine.Hash(raw), Format: "binary", Executable: "gh", MaxBytes: 1024, VersionOutput: "synthetic", RedirectOrigins: []string{}}}
			if mode == "update" || mode == "drift" {
				p.Action = "update"
				p.PreviousPath = old
				p.PreviousSHA256 = engine.Hash(prior)
				if mode == "drift" {
					os.WriteFile(old, []byte("manual-edit"), 0700)
				}
			}
			calls := 0
			check := func() error {
				calls++
				if mode == "expired-before-send" && calls == 2 || mode == "expired-before-publish" && calls == 3 {
					return errors.New("expired fixture grant")
				}
				return nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := Install(ctx, p, check)
			if mode == "install" || mode == "update" {
				if err != nil {
					t.Fatal(err)
				}
				installed, _ := os.ReadFile(dest)
				if string(installed) != string(raw) {
					t.Fatal("wrong installed bytes")
				}
				if _, err = Install(ctx, p, check); err == nil {
					t.Fatal("target overwritten")
				}
			} else {
				if err == nil {
					t.Fatal("drift/expiry ignored")
				}
				if _, err = os.Stat(dest); !os.IsNotExist(err) {
					t.Fatal("blocked install published")
				}
				if _, err = os.Stat(dest + ".workkit-lock"); err != nil {
					t.Fatal("uncertain lock lost")
				}
			}
			current, _ := os.ReadFile(old)
			want := prior
			if mode == "drift" {
				want = []byte("manual-edit")
			}
			if string(current) != string(want) {
				t.Fatal("user-owned prior installation changed")
			}
		})
	}
}
