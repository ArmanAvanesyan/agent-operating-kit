// Package v2 provides compatibility aliases for host observations. The parser
// lives in hostprotocol so the P7 hostbridge package can reuse it without an
// import cycle.
package v2

import "github.com/ArmanAvanesyan/agent-operating-kit/internal/hostprotocol"

const Version = hostprotocol.Version
const MaxFrameBytes = hostprotocol.MaxFrameBytes
const MaxFrames = hostprotocol.MaxFrames

type Dimension = hostprotocol.Dimension
type Event = hostprotocol.Event
type Observation = hostprotocol.Observation

var Replay = hostprotocol.Replay
var ReplayScoped = hostprotocol.ReplayScoped
var DecodeStrict = hostprotocol.DecodeStrict
var ValidHost = hostprotocol.ValidHost
