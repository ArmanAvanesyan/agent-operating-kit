package main

import (
	"encoding/json"
	"os"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/toolisolation"
)

func main() {
	p, q, err := toolisolation.DecodeEnvelope(os.Stdin)
	if err != nil {
		fail()
	}
	r, err := toolisolation.ExecuteWithRootFD(p, q, 3)
	if err != nil {
		fail()
	}
	if json.NewEncoder(os.Stdout).Encode(r) != nil {
		fail()
	}
}

func fail() { _, _ = os.Stderr.WriteString("tool request denied\n"); os.Exit(2) }
