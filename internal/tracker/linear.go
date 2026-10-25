package tracker

import (
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
)

type Linear struct{}

func NewLinear() Adapter        { return Linear{} }
func (Linear) Provider() string { return "linear" }
func (Linear) Initial(s Source, m string, c *Checkpoint) ([]Request, error) {
	return (readsource.Linear{}).Initial(s, m, c)
}
func (Linear) Decode(s Source, r Request, v Response) (PageResult, error) {
	p, e := (readsource.Linear{}).Decode(s, r, v)
	out := compatConvert[PageResult](p)
	out.Next = p.Next
	return out, e
}
