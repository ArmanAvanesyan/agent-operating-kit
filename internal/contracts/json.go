package contracts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

// DecodeJSON reads supporting JSON assets with the same syntax and resource
// limits as canonical documents, without imposing a resource envelope.
func DecodeJSON(data []byte) (any, error) {
	if len(data) > MaxDocumentBytes {
		return nil, fmt.Errorf("JSON exceeds 4 MiB")
	}
	if !utf8.Valid(data) || bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		return nil, fmt.Errorf("invalid JSON encoding")
	}
	if err := scanJSON(data); err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}
