package hostbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestNativeRunnerHelper(t *testing.T) {
	if os.Getenv("AOK_HOST_HELPER") != "1" {
		return
	}
	switch os.Getenv("AOK_HOST_MODE") {
	case "echo":
		var line string
		_, _ = fmt.Fscan(os.Stdin, &line)
		fmt.Fprintln(os.Stdout, line)
	case "large":
		fmt.Fprintln(os.Stdout, strings.Repeat("x", MaxFrameBytes+20))
	case "hang":
		time.Sleep(5 * time.Second)
	case "tree":
		cmd := exec.Command(os.Args[0], "-test.run=^TestNativeRunnerHelper$")
		cmd.Env = append(os.Environ(), "AOK_HOST_MODE=marker-child")
		if cmd.Start() == nil {
			fmt.Fprintln(os.Stdout, cmd.Process.Pid)
			time.Sleep(5 * time.Second)
		}
	case "marker-child":
		time.Sleep(500 * time.Millisecond)
		_ = os.WriteFile(os.Getenv("AOK_MARKER"), []byte("alive"), 0600)
	}
	os.Exit(0)
}

func helperLaunch(t *testing.T, mode string) Launch {
	t.Helper()
	return Launch{Executable: os.Args[0], Args: []string{"-test.run=^TestNativeRunnerHelper$"}, Env: append(os.Environ(), "AOK_HOST_HELPER=1", "AOK_HOST_MODE="+mode), TimeoutSeconds: 3}
}

func TestNativeRunnerEchoAndInvalidSend(t *testing.T) {
	l := helperLaunch(t, "echo")
	p, e := (NativeRunner{}).Start(context.Background(), l)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(map[string]string{"ok": "yes"})
	if e = p.Send(b); e != nil {
		t.Fatal(e)
	}
	c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, e := p.Receive(c)
	if e != nil || string(got) != string(b) {
		t.Fatalf("receive %q %v", got, e)
	}
	if e = p.Send([]byte("not-json")); e == nil {
		t.Fatal("invalid send accepted")
	}
	_ = p.Close()
	_ = p.Wait()
}

func TestNativeRunnerOutputBoundAndReceiveContext(t *testing.T) {
	l := helperLaunch(t, "large")
	l.MaxFrameBytes = 128
	p, e := (NativeRunner{}).Start(context.Background(), l)
	if e != nil {
		t.Fatal(e)
	}
	c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, e = p.Receive(c)
	if e != errLimit {
		t.Fatalf("expected bounded output error, got %v", e)
	}
	_ = p.Stop()
	_ = p.Wait()
	l = helperLaunch(t, "hang")
	p, e = (NativeRunner{}).Start(context.Background(), l)
	if e != nil {
		t.Fatal(e)
	}
	c, cancel = context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, e = p.Receive(c)
	if e == nil || e == errLimit {
		t.Fatalf("expected context cancellation, got %v", e)
	}
	_ = p.Stop()
	_ = p.Wait()
}
