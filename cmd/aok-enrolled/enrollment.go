package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/enrollment"
)

// Preparation is offline and available in stock builds. It does not install
// services or bypass the compiled trust-root gate on active commands.
func runEnrollment(command string, args []string, out, errout io.Writer) error {
	fs := flag.NewFlagSet("aok-enrolled "+command, flag.ContinueOnError)
	fs.SetOutput(errout)
	var directory, instant string
	jsonOutput := false
	if command == "prepare" {
		fs.StringVar(&directory, "out", "", "new private enrollment draft directory")
	} else {
		fs.StringVar(&directory, "bundle", "", "local enrollment preparation directory")
		fs.StringVar(&instant, "as-of", "", "required RFC3339 inspection instant")
		fs.BoolVar(&jsonOutput, "json", false, "emit structured blockers")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return usageError(err)
	}
	if directory == "" || fs.NArg() != 0 {
		return usageError(errors.New("prepare requires --out; preflight requires --bundle and --as-of"))
	}
	if command == "prepare" {
		if enrollment.Prepare(directory) != nil {
			return blockedError(errors.New("enrollment preparation failed; output must be a new safe directory"))
		}
		return json.NewEncoder(out).Encode(map[string]any{"version": enrollment.Version, "status": "drafts-prepared", "offlineOnly": true, "authorizationGranted": false, "executionReady": false, "productionEligible": false})
	}
	at, err := time.Parse(time.RFC3339Nano, instant)
	if err != nil {
		return usageError(errors.New("preflight requires --as-of RFC3339"))
	}
	r, err := enrollment.Check(directory, at.UTC())
	if err != nil {
		return blockedError(errors.New("enrollment package invalid or unsafe; preflight stopped"))
	}
	if jsonOutput {
		err = json.NewEncoder(out).Encode(r)
	} else {
		_, err = fmt.Fprintf(out, "Offline enrollment preflight: %s\nAuthorization granted: false; execution ready: false; production eligible: false\n", r.Status)
		for _, finding := range r.Findings {
			if err != nil {
				break
			}
			_, err = fmt.Fprintf(out, "%s [%s] %s: %s\n", finding.ID, finding.Stage, finding.Status, finding.Detail)
		}
	}
	if err != nil {
		return err
	}
	if r.Status != "valid" {
		return blockedError(errors.New("enrollment remains blocked; independent live qualification and operator inputs required"))
	}
	return nil
}
