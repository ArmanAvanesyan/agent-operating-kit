// Package schemas supplies pinned offline WorkKit schemas.
package schemas

import "embed"

//go:embed *.json
var Files embed.FS
