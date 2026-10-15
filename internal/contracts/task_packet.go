package contracts

import (
	"bytes"
	"fmt"
	"unicode/utf8"
)

// ValidateTaskPacket checks an existing neutral task packet without changing it
// or executing any of its declared verification commands.
func ValidateTaskPacket(data []byte) error {
	if len(data) > MaxDocumentBytes || !utf8.Valid(data) || bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		return fmt.Errorf("task packet exceeds bounds or has invalid encoding")
	}
	if err := scanJSON(data); err != nil {
		return err
	}
	return validateSchema("TaskPacket", data)
}
