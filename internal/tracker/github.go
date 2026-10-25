package tracker

import (
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
)

type GitHub struct{}

func NewGitHub() Adapter        { return GitHub{} }
func (GitHub) Provider() string { return "github" }
func (GitHub) Initial(s Source, m string, c *Checkpoint) ([]Request, error) {
	return (readsource.GitHub{}).Initial(s, m, c)
}
func (GitHub) Decode(s Source, r Request, v Response) (PageResult, error) {
	p, e := (readsource.GitHub{}).Decode(s, r, v)
	out := compatConvert[PageResult](p)
	out.Next = p.Next
	return out, e
}
