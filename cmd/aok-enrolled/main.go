// aok-enrolled is a separately built protected service host. The default
// binary intentionally has no compiled trust root and cannot start services.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/bootstrap"
)

// Set at build time with -ldflags '-X main.compiledRootSHA256=<root digest>'.
// An empty default blocks all serve commands before any secret resolution.
var compiledRootSHA256 string

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		var coded *commandError
		if errors.As(err, &coded) {
			os.Exit(coded.code)
		}
		os.Exit(1)
	}
}

type commandError struct {
	code int
	err  error
}

func (e *commandError) Error() string { return e.err.Error() }
func (e *commandError) Unwrap() error { return e.err }
func usageError(err error) error      { return &commandError{code: 2, err: err} }
func blockedError(err error) error    { return &commandError{code: 1, err: err} }

func run(args []string, stdout, stderr *os.File) error {
	if len(args) == 0 {
		return usageError(errors.New("usage: aok-enrolled prepare|preflight|inspect|authority serve|verifier serve|anchor serve|worker|qualify"))
	}
	command := args[0]
	if command == "prepare" || command == "preflight" {
		return runEnrollment(command, args[1:], stdout, stderr)
	}
	serveService := ""
	flagArgs := args[1:]
	if command == "worker" || command == "qualify" {
		serveService = command
		command = "serve"
		flagArgs = args[1:]
	} else if command == "authority" || command == "verifier" || command == "anchor" {
		if len(args) < 2 || args[1] != "serve" {
			return usageError(errors.New("usage: aok-enrolled authority serve|verifier serve|anchor serve --config FILE --root FILE"))
		}
		serveService = command
		command = "serve"
		flagArgs = args[2:]
	}
	if command != "inspect" && command != "serve" {
		return usageError(errors.New("unknown protected service command"))
	}
	fs := flag.NewFlagSet("aok-enrolled "+command, flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "signed protected service envelope")
	rootPath := fs.String("root", "", "externally pinned Ed25519 public root file")
	asOf := fs.String("as-of", "", "explicit RFC3339 instant for offline inspection")
	if err := fs.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return usageError(err)
	}
	if *configPath == "" || *rootPath == "" || fs.NArg() != 0 {
		return usageError(errors.New("--config and --root are required"))
	}
	if command == "inspect" {
		if *asOf == "" {
			return usageError(errors.New("inspect requires --as-of RFC3339"))
		}
		at, parseErr := time.Parse(time.RFC3339Nano, *asOf)
		if parseErr != nil {
			return usageError(errors.New("inspect requires --as-of RFC3339"))
		}
		out, err := bootstrap.Inspect(*configPath, *rootPath, compiledRootSHA256, at.UTC())
		if err != nil {
			return blockedError(errors.New("protected config inspection failed"))
		}
		b, err := bootstrap.RedactedInspectJSON(out)
		if err != nil {
			return err
		}
		_, err = stdout.Write(b)
		return err
	}
	if *asOf != "" {
		return usageError(errors.New("--as-of is only valid with inspect"))
	}
	// Keep the stock build inert. This check precedes config parsing and every
	// resolver call; linker injection is required for an enrolled deployment.
	if !isSHA256(compiledRootSHA256) {
		return blockedError(errors.New("protected service disabled: executable root pin is empty or invalid"))
	}
	if serveService == "worker" || serveService == "qualify" {
		var worker *bootstrap.WorkerRun
		var err error
		if serveService == "qualify" {
			worker, err = bootstrap.BuildQualification(*configPath, *rootPath, compiledRootSHA256, time.Now().UTC())
		} else {
			worker, err = bootstrap.BuildWorker(*configPath, *rootPath, compiledRootSHA256, time.Now().UTC())
		}
		if err != nil {
			return blockedError(errors.New("protected worker bootstrap failed"))
		}
		defer worker.Close()
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if code := worker.Run(ctx, stdout, stderr); code != 0 {
			return blockedError(fmt.Errorf("protected worker command exited with status %d", code))
		}
		return nil
	}
	p, err := bootstrap.Build(*configPath, *rootPath, compiledRootSHA256, time.Now().UTC())
	if err != nil {
		return blockedError(errors.New("protected service bootstrap failed"))
	}
	defer p.Close()
	if !strings.EqualFold(p.Service(), serveService) {
		return blockedError(errors.New("signed service selection does not match command"))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := bootstrap.Serve(ctx, p); err != nil {
		return blockedError(errors.New("protected service stopped or failed"))
	}
	return nil
}

func isSHA256(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha256.Size && strings.ToLower(s) == s
}
