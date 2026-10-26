//go:build darwin || linux

package sdk

import (
	"context"
	"encoding/json"
	"errors"
	i "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/integrationv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/platformfs"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

// ServeIntegration exposes only typed private control messages. There is no TCP,
// public webhook listener, task-selected handler or provider URL endpoint.
func (s *Service) ServeIntegration(ctx context.Context, workspace, configuration, policy, enrollment string) (i.Report, error) {
	w, e := s.newIntegrationWorker(ctx, workspace, configuration, policy, enrollment)
	if e != nil {
		return i.Report{}, e
	}
	return w.serve(ctx)
}
func (w *integrationWorker) serve(ctx context.Context) (i.Report, error) {
	r := integrationReport("IntegrationService", w.en.Store)
	socket := w.configuration.Listen
	parent := filepath.Dir(socket)
	for p := parent; ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return r, errors.New("unsafe socket ancestor")
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	st, e := os.Stat(parent)
	if e != nil || st.Mode().Perm()&0077 != 0 || !integrationDirectoryOwner(st) {
		return r, errors.New("private owner-only socket directory required")
	}
	if _, e = os.Lstat(socket); !os.IsNotExist(e) {
		return r, errors.New("fresh socket path required; no stale socket takeover")
	}
	listener, e := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if e != nil {
		return r, e
	}
	listener.SetUnlinkOnClose(false)
	defer listener.Close()
	if e = os.Chmod(socket, 0600); e != nil {
		return r, e
	}
	bound, e := os.Lstat(socket)
	if e != nil {
		return r, e
	}
	defer func() {
		current, err := os.Lstat(socket)
		if err == nil && os.SameFile(bound, current) {
			os.Remove(socket)
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(w.configuration.Limits.ProcessSeconds)*time.Second)
	defer cancel()
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if w.heartbeat(ctx) != nil {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { cancel(); <-heartbeatDone }()
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			conn, err := listener.AcceptUnix()
			if err != nil {
				return
			}
			func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				if integrationPeerOwner(conn) != nil {
					return
				}
				raw, err := io.ReadAll(io.LimitReader(conn, 4<<20+1))
				if err != nil || len(raw) > 4<<20 {
					return
				}
				var c i.Command
				var result i.Report
				if DecodeRead(raw, &c) != nil {
					result = integrationReport("IntegrationControl", w.en.Store)
					result.Status = "blocked"
				} else {
					var commandEn i.Enrollment
					if DecodeRead(c.Enrollment, &commandEn) != nil || commandEn.Store != w.en.Store || commandEn.WorkerID != w.en.WorkerID || commandEn.ProcessID != w.en.ProcessID || commandEn.ProcessStart != w.en.ProcessStart || c.RouteID == "" || commandEn.Fence != w.fence(c.RouteID) || w.configuration.Routes[c.RouteID] == "" {
						result = integrationReport("IntegrationControl", i.Store{})
						result.Status = "blocked"
						json.NewEncoder(conn).Encode(result)
						return
					}
					peerPID, peerErr := integrationPeerPID(conn)
					peerStart, peerStartErr := platformfs.ProcessStartFor(peerPID)
					if peerErr != nil || peerStartErr != nil || commandEn.CallerProcessID != peerPID || commandEn.CallerProcessStart != peerStart {
						result = integrationReport("IntegrationControl", w.en.Store)
						result.Status = "blocked"
						json.NewEncoder(conn).Encode(result)
						return
					}
					commandLimit := 5 * time.Second
					if c.Action == "submit" {
						commandLimit = 5 * time.Minute
						conn.SetDeadline(time.Now().Add(commandLimit))

					}
					commandCtx, stop := context.WithTimeout(ctx, commandLimit)
					if c.Action == "submit" {
						if err = integrationLock(commandCtx, &w.mu); err != nil {
							stop()
							result = integrationReport("IntegrationControl", w.en.Store)
							result.Status = "blocked"
							result.RouteID = c.RouteID
							result.Fence = commandEn.Fence
							result.RequestSHA256 = c.SubjectSHA256
							json.NewEncoder(conn).Encode(result)
							return
						}
						defer w.mu.Unlock()
					}
					commandCtx = context.WithValue(commandCtx, integrationBudgetKey{}, &integrationBudget{limits: w.configuration.Limits})
					result, err = w.svc.IntegrationCommand(commandCtx, c)
					stop()
					if err != nil {
						if result.Version == "" {
							result = integrationReport("IntegrationControl", w.en.Store)
						}
						var failure *Error
						if errors.As(err, &failure) && failure.ExitCode == 5 || len(result.Operations) > 0 {
							result.Status = "unknown"
						} else {
							result.Status = "blocked"
						}
					}
				}
				result.RequestSHA256 = c.SubjectSHA256
				if result.Version != "" && result.Store == w.en.Store {
					result.RouteID = c.RouteID
					result.Fence = commandEnFence(c)
				}
				json.NewEncoder(conn).Encode(result)
			}()
		}
	}()
	defer func() { listener.Close(); <-acceptDone }()
	for {
		if err := w.current(ctx); err != nil {
			return r, err
		}
		cycle, err := w.cycle(ctx)
		r = cycle
		if err != nil {
			return r, err
		}
		delay := time.Duration(w.configuration.Limits.PollSeconds) * time.Second
		// Stable per-cycle jitter; replay/tests do not depend on global randomness.
		if j := w.configuration.Limits.JitterSeconds; j > 0 {
			delay += time.Duration(w.cycles%(j+1)) * time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return r, ctx.Err()
		case <-timer.C:
		}
	}
}

// ForwardIntegration never opens the journal or installs authority. The owner
// verifies the complete signed action and all separate operation grants.
func ForwardIntegration(ctx context.Context, socket string, c i.Command) (i.Report, error) {
	var report i.Report
	if !filepath.IsAbs(socket) || filepath.Clean(socket) != socket || c.SubjectSHA256 != IntegrationCommandSubject(c) {
		return report, errors.New("exact private IPC command required")
	}
	st, e := os.Lstat(socket)
	if e != nil || st.Mode()&os.ModeSocket == 0 || st.Mode().Perm()&0077 != 0 || !integrationDirectoryOwner(st) {
		return report, errors.New("private same-owner socket required")
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, e := dialer.DialContext(ctx, "unix", socket)
	if e != nil {
		return report, e
	}
	defer conn.Close()
	unixConn, ok := conn.(*net.UnixConn)
	if !ok || integrationPeerOwner(unixConn) != nil {
		return report, errors.New("worker peer identity failed")
	}
	limit := 5 * time.Second
	if c.Action == "submit" {
		limit = 5 * time.Minute
	}
	conn.SetDeadline(time.Now().Add(limit))
	var enrollment i.Enrollment
	if DecodeRead(c.Enrollment, &enrollment) != nil || enrollment.ProcessID < 1 || enrollment.ProcessStart == "" {
		return report, errors.New("process-bound owner enrollment required")
	}
	pid, err := integrationPeerPID(unixConn)
	if err != nil || pid != enrollment.ProcessID {
		return report, errors.New("socket peer is not the enrolled worker")
	}
	start, err := platformfs.ProcessStartFor(pid)
	if err != nil || start != enrollment.ProcessStart {
		return report, errors.New("worker process incarnation changed")
	}
	if e = json.NewEncoder(conn).Encode(c); e != nil {
		if c.Action == "submit" {
			report = i.Report{Version: i.Version, Kind: "IntegrationControl", Status: "unknown", Store: enrollment.Store, RouteID: c.RouteID, Fence: enrollment.Fence, RequestSHA256: c.SubjectSHA256}
			return report, operationFailure("integration.ipc-uncertain", "submission may have reached the worker; inspect/reconcile without resend", 5, e)
		}
		return report, e
	}
	if e = unixConn.CloseWrite(); e != nil {
		if c.Action == "submit" {
			report = i.Report{Version: i.Version, Kind: "IntegrationControl", Status: "unknown", Store: enrollment.Store, RouteID: c.RouteID, Fence: enrollment.Fence, RequestSHA256: c.SubjectSHA256}
			return report, operationFailure("integration.ipc-uncertain", "submission may have reached the worker; inspect/reconcile without resend", 5, e)
		}
		return report, e
	}
	raw, e := io.ReadAll(io.LimitReader(conn, 4<<20+1))
	if e != nil || len(raw) > 4<<20 {
		if c.Action == "submit" {
			report = i.Report{Version: i.Version, Kind: "IntegrationControl", Status: "unknown", Store: enrollment.Store, RouteID: c.RouteID, Fence: enrollment.Fence, RequestSHA256: c.SubjectSHA256}
			return report, operationFailure("integration.ipc-uncertain", "worker response unavailable; inspect/reconcile without resend", 5, e)
		}
		return report, errors.New("IPC response bound")
	}
	if e = DecodeRead(raw, &report); e != nil {
		if c.Action == "submit" {
			report = i.Report{Version: i.Version, Kind: "IntegrationControl", Status: "unknown", Store: enrollment.Store, RouteID: c.RouteID, Fence: enrollment.Fence, RequestSHA256: c.SubjectSHA256}
			return report, operationFailure("integration.ipc-uncertain", "malformed worker response after submission; reconcile without resend", 5, e)
		}
		return report, e
	}
	if report.RequestSHA256 != c.SubjectSHA256 || report.Store != enrollment.Store || report.RouteID != c.RouteID || report.Fence != enrollment.Fence {
		if c.Action == "submit" {
			report = i.Report{Version: i.Version, Kind: "IntegrationControl", Status: "unknown", Store: enrollment.Store, RouteID: c.RouteID, Fence: enrollment.Fence, RequestSHA256: c.SubjectSHA256}
			return report, operationFailure("integration.ipc-uncertain", "worker response scope changed after submission; reconcile without resend", 5, nil)
		}
		return report, errors.New("IPC response scope or worker fence changed")
	}
	if report.Version != i.Version || report.AuthorizationGranted || report.ExecutionReady || report.MigrationActivated || report.ProductionActivated {
		if c.Action == "submit" {
			return i.Report{Version: i.Version, Kind: "IntegrationControl", Status: "unknown", Store: enrollment.Store, RouteID: c.RouteID, Fence: enrollment.Fence, RequestSHA256: c.SubjectSHA256}, operationFailure("integration.ipc-uncertain", "invalid worker response after submission; reconcile without resend", 5, nil)
		}
		return i.Report{}, errors.New("invalid IPC report")
	}
	return report, nil
}

func commandEnFence(c i.Command) int64 {
	var en i.Enrollment
	if DecodeRead(c.Enrollment, &en) != nil {
		return 0
	}
	return en.Fence
}
