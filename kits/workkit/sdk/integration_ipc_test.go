//go:build darwin || linux

package sdk

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	i "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/integrationv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func p7socketRoot(t *testing.T) string {
	t.Helper()
	root, e := os.MkdirTemp("", "wk7-")
	if e != nil {
		t.Fatal(e)
	}
	root, e = filepath.EvalSymlinks(root)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	return root
}
func TestP7PrivateIPCAndIdleRenewal(t *testing.T) {
	socket := filepath.Join(p7socketRoot(t), "control.sock")
	svc, backend, workspace, config, policy, enrollment, p, en, key := p7workerFixture(t, socket)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, e := svc.ServeIntegration(ctx, workspace, config, policy, enrollment); done <- e }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		backend.mu.Lock()
		ready := backend.records > 0
		backend.mu.Unlock()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("service did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	command := i.Command{Version: i.Version, Kind: "IntegrationCommand", Action: "status", RouteID: backend.plan.plan.RouteID}
	command.SubjectSHA256 = IntegrationCommandSubject(command)
	praw, _ := os.ReadFile(policy)
	grant := en
	grant.ID = "status-one-use"
	grant.CallerProcessID = os.Getpid()
	grant.CallerProcessStart, _ = CurrentProcessStart()
	grant.Purpose = "local-inspect"
	grant.RouteID = command.RouteID
	grant.Fence = 1
	grant.SubjectSHA256 = command.SubjectSHA256
	grant.PolicySHA256 = SHA256(praw)
	grant.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, IntegrationSigningBytes(grant)))
	command.Policy = praw
	command.Enrollment = engine.Encode(grant)
	r, e := ForwardIntegration(context.Background(), socket, command)
	if e != nil || r.Status != "ok" || r.RouteID != command.RouteID {
		t.Fatal(r, e)
	}
	// The default60s idle period exceeds the30s lease; observe the10s heartbeat
	// renewing while no new cycle or send occurs.
	until := time.Now().Add(13 * time.Second)
	for {
		backend.mu.Lock()
		claims, sends := backend.claims, backend.sends
		backend.mu.Unlock()
		if sends != 0 {
			t.Fatal("review-first dispatched")
		}
		if claims >= 2 {
			break
		}
		if time.Now().After(until) {
			t.Fatal("idle lease not renewed")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case e = <-done:
		if e == nil {
			t.Fatal("expected bounded cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("helper/socket cleanup exceeded bound")
	}
	if _, e = os.Lstat(socket); !os.IsNotExist(e) {
		t.Fatal("owned socket not cleaned")
	}
	_ = p
}
func TestP7IPCSubmissionLostReplyIsUncertain(t *testing.T) {
	socket := filepath.Join(p7socketRoot(t), "lost.sock")
	listener, e := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	os.Chmod(socket, 0600)
	_, en, _, praw := p7authority(t, SHA256([]byte("submit")), "submit-batch")
	en.ProcessID = os.Getpid()
	en.ProcessStart, _ = CurrentProcessStart()
	command := i.Command{Version: i.Version, Kind: "IntegrationCommand", Action: "submit", RouteID: "route", Policy: praw, Enrollment: engine.Encode(en)}
	command.SubjectSHA256 = IntegrationCommandSubject(command)
	go func() {
		conn, err := listener.AcceptUnix()
		if err == nil {
			defer conn.Close()
			buffer := make([]byte, 8192)
			conn.Read(buffer)
		}
	}()
	_, err := ForwardIntegration(context.Background(), socket, command)
	var failure *Error
	if !errors.As(err, &failure) || failure.ExitCode != 5 {
		t.Fatal("lost reply reported safe/ retryable", err)
	}
}
