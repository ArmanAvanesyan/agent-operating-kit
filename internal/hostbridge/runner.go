package hostbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"sync"
	"time"
)

var errLimit = errors.New("host process output limit exceeded")
var errSend = errors.New("invalid host process input")

// NativeRunner starts a bounded native subprocess using a fixed argv vector.
type NativeRunner struct{}

func (NativeRunner) Start(ctx context.Context, l Launch) (Process, error) {
	if ctx == nil || l.Executable == "" || len(l.Args) == 0 || l.TimeoutSeconds <= 0 {
		return nil, errors.New("invalid host launch")
	}
	frame, total := l.MaxFrameBytes, l.MaxTotalBytes
	if frame <= 0 || frame > MaxFrameBytes {
		frame = MaxFrameBytes
	}
	if total <= 0 || total > MaxCaptureBytes {
		total = MaxCaptureBytes
	}
	if ctx.Err() != nil {
		return nil, errors.New("host launch canceled")
	}
	childCtx, cancel := context.WithTimeout(ctx, time.Duration(l.TimeoutSeconds)*time.Second)
	cmd := exec.Command(l.Executable, l.Args...)
	cmd.Dir, cmd.Env, cmd.Stderr = l.Dir, append([]string(nil), l.Env...), io.Discard
	in, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, errors.New("host process unavailable")
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, errors.New("host process unavailable")
	}
	if err = configureProcess(cmd); err != nil {
		cancel()
		return nil, errors.New("host process unavailable")
	}
	if err = cmd.Start(); err != nil {
		cancel()
		return nil, errors.New("host process unavailable")
	}
	if err = attachProcess(cmd); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		cancel()
		return nil, errors.New("host process unavailable")
	}
	p := &nativeProcess{cmd: cmd, stdin: in, stdout: out, cancel: cancel, frame: frame, total: total, notify: make(chan struct{}, 1), done: make(chan struct{}), readerDone: make(chan struct{})}
	p.platformHandle = processHandle(cmd)
	go p.readFrames()
	go func() {
		<-p.readerDone
		err := cmd.Wait()
		if err != nil {
			p.waitErr = errors.New("host process exited unsuccessfully")
		}
		finishProcess(cmd, p.platformHandle)
		close(p.done)
		cancel()
	}()
	go func() {
		select {
		case <-childCtx.Done():
			_ = p.Stop()
		case <-p.done:
		}
	}()
	return p, nil
}

type readResult struct {
	b   []byte
	err error
}
type nativeProcess struct {
	cmd            *exec.Cmd
	stdin          io.WriteCloser
	stdout         io.ReadCloser
	cancel         context.CancelFunc
	platformHandle uintptr
	frame, total   int
	mu             sync.Mutex
	used           int
	sendMu         sync.Mutex
	stopOnce       sync.Once
	frames         []readResult
	queueMu        sync.Mutex
	notify         chan struct{}
	done           chan struct{}
	readerDone     chan struct{}
	waitErr        error
}

func (p *nativeProcess) account(n int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if n > p.total-p.used {
		return false
	}
	p.used += n
	return true
}
func (p *nativeProcess) Send(b []byte) error {
	if len(b) == 0 || len(b) > p.frame || !json.Valid(b) {
		_ = p.Stop()
		return errSend
	}
	p.sendMu.Lock()
	defer p.sendMu.Unlock()
	if !p.account(len(b) + 1) {
		_ = p.Stop()
		return errLimit
	}
	line := append(append([]byte(nil), b...), '\n')
	if _, err := p.stdin.Write(line); err != nil {
		return errors.New("host process input failed")
	}
	return nil
}
func (p *nativeProcess) readFrames() {
	defer close(p.readerDone)
	r := bufio.NewReaderSize(p.stdout, 32*1024)
	for {
		var buf []byte
		for {
			part, err := r.ReadSlice('\n')
			if len(buf)+len(part) > p.frame+1 || !p.account(len(part)) {
				_ = p.Stop()
				_ = p.publish(readResult{err: errLimit})
				return
			}
			buf = append(buf, part...)
			if err == nil {
				buf = buf[:len(buf)-1]
				if !p.publish(readResult{b: buf}) {
					return
				}
				break
			}
			if err == bufio.ErrBufferFull {
				continue
			}
			if err == io.EOF {
				if len(buf) > 0 {
					if !p.publish(readResult{b: buf}) {
						return
					}
				}
				_ = p.publish(readResult{err: io.EOF})
				return
			}
			_ = p.publish(readResult{err: errors.New("host process output failed")})
			return
		}
	}
}
func (p *nativeProcess) publish(r readResult) bool {
	p.queueMu.Lock()
	p.frames = append(p.frames, r)
	p.queueMu.Unlock()
	select {
	case p.notify <- struct{}{}:
	default:
	}
	return true
}
func (p *nativeProcess) Receive(ctx context.Context) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("invalid receive context")
	}
	for {
		p.queueMu.Lock()
		if len(p.frames) > 0 {
			r := p.frames[0]
			p.frames[0] = readResult{}
			p.frames = p.frames[1:]
			p.queueMu.Unlock()
			if r.err == errLimit {
				_ = p.Stop()
			}
			return r.b, r.err
		}
		p.queueMu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-p.notify:
		}
	}
}
func (p *nativeProcess) Stop() error {
	p.stopOnce.Do(func() { p.cancel(); _ = stopProcess(p.cmd, p.platformHandle) })
	return nil
}
func (p *nativeProcess) Close() error {
	_ = p.Stop()
	_ = p.stdin.Close()
	<-p.readerDone
	<-p.done
	return nil
}
func (p *nativeProcess) Wait() error { <-p.done; return p.waitErr }
