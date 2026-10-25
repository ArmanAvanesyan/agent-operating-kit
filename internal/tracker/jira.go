package tracker

import (
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
)

type Jira struct{}

func NewJira() Adapter        { return Jira{} }
func (Jira) Provider() string { return "jira" }
func (Jira) Initial(s Source, m string, c *Checkpoint) ([]Request, error) {
	return (readsource.Jira{}).Initial(s, m, c)
}
func (Jira) Decode(s Source, r Request, v Response) (PageResult, error) {
	p, e := (readsource.Jira{}).Decode(s, r, v)
	out := compatConvert[PageResult](p)
	out.Next = p.Next
	return out, e
}
