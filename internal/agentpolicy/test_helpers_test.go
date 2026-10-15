package agentpolicy

import "os"

func osReadProfileFixture() ([]byte, error) {
	return os.ReadFile("../contracts/testdata/execution-profile.json")
}
