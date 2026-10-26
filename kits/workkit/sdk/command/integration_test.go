package command

import (
	i "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/integrationv1"
	"testing"
)

func TestP7CLIIntentCannotForwardEffectsThroughStatus(t *testing.T) {
	for _, action := range []string{"submit", "ack", "events", "ingest"} {
		if integrationCommandMatches("status", i.Command{Action: action, Batch: &i.BatchRequest{Action: "sync-run"}}) {
			t.Fatal("status forwarded effect", action)
		}
	}
	if !integrationCommandMatches("status", i.Command{Action: "status"}) {
		t.Fatal("status rejected")
	}
	for _, verb := range []string{"run", "pause", "resume", "reconcile"} {
		if !integrationCommandMatches(verb, i.Command{Action: "submit", Batch: &i.BatchRequest{Action: "sync-" + verb}}) {
			t.Fatal(verb)
		}
		if integrationCommandMatches(verb, i.Command{Action: "ack"}) {
			t.Fatal("ack accepted", verb)
		}
	}
	if integrationCommandMatches("pause", i.Command{Action: "submit", Batch: &i.BatchRequest{Action: "sync-run"}}) {
		t.Fatal("pause dispatched run")
	}
}
