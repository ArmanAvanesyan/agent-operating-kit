package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
)

const verifyHelp = `aok verify import --manifest FILE --policy FILE [--source-root DIRECTORY] --out NEW
 aok verify check BUNDLE --policy FILE --as-of RFC3339 [--json]
 aok verify inspect BUNDLE --policy FILE --as-of RFC3339 [--json]
 aok verify run BUNDLE --policy FILE --out NEW [--json]
 aok verify accept BUNDLE --receipt FILE --policy FILE --out NEW [--json]
Acceptance consumes an externally signed receipt; this command never creates consent or stores signing keys.
Command verifiers require independently qualified controls; both P6 host live gates remain blocked.`

func phase8Read(path string, target any) error {
	b, e := modelcatalog.ReadFile(path)
	if e != nil {
		return e
	}
	return usage.Decode(b, target)
}
func phase8Print(out io.Writer, result any) (int, error) {
	e := json.NewEncoder(out)
	e.SetIndent("", "  ")
	if err := e.Encode(result); err != nil {
		return 1, err
	}
	return 0, nil
}
func phase8AsOf(value string) (time.Time, error) {
	at, e := time.Parse(time.RFC3339Nano, value)
	if e != nil || at.IsZero() {
		return at, fmt.Errorf("--as-of requires RFC3339")
	}
	return at, nil
}
func runVerify(args []string, out, errout io.Writer) (int, error) {
	return runVerifyServices(args, out, errout, Phase9Services{})
}
func runVerifyServices(args []string, out, errout io.Writer, services Phase9Services) (int, error) {
	if len(args) == 0 {
		fmt.Fprintln(errout, verifyHelp)
		return 2, nil
	}
	if args[0] == "--help" || args[0] == "help" || len(args) > 1 && args[1] == "--help" {
		fmt.Fprintln(out, verifyHelp)
		return 0, nil
	}
	op := args[0]
	args = args[1:]
	bundle := ""
	keys, ok := map[string][]string{"import": {"manifest", "policy", "out", "source-root"}, "check": {"policy", "as-of"}, "inspect": {"policy", "as-of"}, "run": {"policy", "out"}, "accept": {"policy", "receipt", "out"}}[op]
	if !ok {
		return 2, fmt.Errorf("unknown verification command")
	}
	if op != "import" {
		if len(args) == 0 {
			return 2, fmt.Errorf("bundle required")
		}
		bundle = args[0]
		args = args[1:]
	}
	v, _, e := selectionFlags(args, keys)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 2, nil
	}
	for _, k := range keys {
		if k == "source-root" {
			continue
		}
		if v["--"+k] == "" {
			fmt.Fprintln(errout, "missing --"+k)
			return 2, nil
		}
	}
	pb, e := modelcatalog.ReadFile(v["--policy"])
	if e != nil {
		return 1, e
	}
	p, e := verification.DecodePolicy(pb)
	if e != nil {
		return 1, e
	}
	if op == "import" {
		b, e := modelcatalog.ReadFile(v["--manifest"])
		if e != nil {
			return 1, e
		}
		m, e := verification.DecodeManifest(b)
		if e != nil {
			return 1, e
		}
		source := v["--source-root"]
		if source == "" {
			source = filepath.Join(filepath.Dir(v["--manifest"]), "source")
		}
		if _, err := os.Lstat(v["--out"]); !os.IsNotExist(err) {
			return 1, fmt.Errorf("output must be a new directory")
		}
		c, e := verification.ImportCandidate(m, source, v["--out"])
		if e != nil {
			return 1, e
		}
		r, e := verification.Check(c, p)
		if e != nil {
			return 1, e
		}
		_, e = phase8Print(out, r)
		if r.Disposition == "blocked" {
			return 1, e
		}
		return 0, e
	}
	c, e := verification.LoadCandidate(bundle)
	if e != nil {
		return 1, e
	}
	if op == "check" || op == "inspect" {
		if _, e = phase8AsOf(v["--as-of"]); e != nil {
			fmt.Fprintln(errout, e)
			return 2, nil
		}
		r, e := verification.Check(c, p)
		if e != nil {
			return 1, e
		}
		_, e = phase8Print(out, r)
		if r.Disposition == "blocked" {
			return 1, e
		}
		return 0, e
	}
	if op == "run" {
		if services.VerifierControls != nil {
			return phase9VerifyRun(c, p, v["--out"], bundle, v["--policy"], services, out)
		}
		// The public CLI has no trusted verifier-control transport yet. Capturing a
		// blocked report is safe; a policy boolean cannot qualify an executor.
		r := map[string]any{"version": verification.Version, "status": "blocked", "reason": "independently qualified verifier control transport is required", "authorizationGranted": false, "executionReady": false}
		if e = evidence.WriteJSON(v["--out"], "verification-report.json", r, bundle, v["--policy"]); e != nil {
			return 1, e
		}
		_, e = phase8Print(out, r)
		return 1, e
	}
	var receipt verification.AcceptanceReceipt
	if e = phase8Read(v["--receipt"], &receipt); e != nil {
		return 1, e
	}
	if e = verification.VerifyAcceptance(receipt, p.Issuers, time.Now().UTC(), time.Duration(p.MaxAgeSeconds)*time.Second); e != nil {
		return 1, e
	}
	if e = verification.ValidateAcceptanceBinding(receipt, c, p); e != nil {
		return 1, e
	}
	if e = evidence.WriteJSON(v["--out"], "acceptance.json", receipt, bundle, v["--receipt"], v["--policy"]); e != nil {
		return 1, e
	}
	return phase8Print(out, receipt)
}
