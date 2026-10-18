#!/usr/bin/env python3
"""Prepare one approved isolated account session; no model or manager input.

Explicit flags attest approval for required terms with optional sharing disabled
and trust limited to the owned temporary root. Code input uses a private file.
"""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import time

SPEC = importlib.util.spec_from_file_location(
    "agy_account_local", Path(__file__).with_name("probe_agy_local_manager.py"))
local = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(local)
login = local.login


class ApprovedAccountSetup:
    """Reuse the reviewed exact setup gates; never inspect header row indexes."""
    def __init__(self):
        self.snapshot = None
        self.digest = None
        self.changed = time.monotonic()
        self.actions = set()
        self.master = self.root = self.audit_path = None
        self.original_state = login.state_from_bytes
        self.original_select = login.select.select
        self.original_capture = login._capture_login

    def observe(self, wire):
        state = self.original_state(wire)
        if state.get("terminalAmbiguityObserved"):
            self.snapshot = None
            return state
        text = login.AGY.terminal_text(wire, width=login.LOGIN_WIDTH, height=login.LOGIN_HEIGHT)
        digest = hashlib.sha256(text.encode()).hexdigest()
        if digest != self.digest:
            self.changed = time.monotonic()
            self.digest = digest
        self.snapshot = (state, text, digest)
        return state

    def select(self, readers, writers, errors, timeout):
        ready = self.original_select(readers, writers, errors, timeout)
        if ready[0] or self.snapshot is None or time.monotonic() - self.changed < .7:
            return ready
        state, text, digest = self.snapshot
        action = local.onboarding_action(text, state, self.root)
        # The original code-entry capture owns the single color continuation.
        if action and action[0] != "color" and action[0] not in self.actions and len(self.actions) < 7:
            name, value = action
            login.append_audit(self.audit_path, "approved-account-onboarding-action",
                               action=name, screenSHA256=digest, scope="owned-isolated-root-only")
            os.write(self.master, value)
            self.actions.add(name)
            self.snapshot = None
            self.changed = time.monotonic()
        return ready

    def capture(self, master, *args, **kwargs):
        self.master, self.root = master, Path(kwargs["case_root"])
        self.audit_path = kwargs["audit_path"]
        try:
            login.state_from_bytes, login.select.select = self.observe, self.select
            return self.original_capture(master, *args, **kwargs)
        finally:
            login.state_from_bytes, login.select.select = self.original_state, self.original_select


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--authorization-code-file", type=Path, required=True)
    parser.add_argument("--evidence-out", type=Path, required=True)
    parser.add_argument("--accept-required-terms", action="store_true", required=True)
    parser.add_argument("--disable-optional-sharing", action="store_true", required=True)
    parser.add_argument("--trust-owned-fixtures", action="store_true", required=True)
    args = parser.parse_args(argv)
    try:
        fd = local.open_evidence_output(args.evidence_out)
    except (OSError, ValueError):
        parser.error("evidence must be a new file in canonical nonsymlink checkout directories")
    setup = ApprovedAccountSetup()
    old_capture, old_mode = login._capture_login, login.MANUAL_OAUTH_MODE
    try:
        login._capture_login, login.MANUAL_OAUTH_MODE = setup.capture, True
        result = login.prepare(timeout=300, authorization_code_file=args.authorization_code_file)
        report = {"kind": "aok-agy-account-login-observation/v1", **result,
                  "approvedActions": sorted(setup.actions),
                  "operatorSourceSHA256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                  "backendAuthentication": "not-independently-verified",
                  "modelInvocation": "not-run", "nativeLoading": "not-verified"}
        with os.fdopen(fd, "w") as out:
            fd = None
            json.dump(report, out, indent=2, sort_keys=True)
            out.write("\n")
        print(json.dumps({"status": result["status"], "root": result["root"],
                          "auditRoot": result["auditRoot"]}), flush=True)
        return 0 if result["status"] == "prompt-ready" else 1
    finally:
        login._capture_login, login.MANUAL_OAUTH_MODE = old_capture, old_mode
        if fd is not None:
            os.close(fd)


if __name__ == "__main__":
    raise SystemExit(main())
