package tracker

import (
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
)

type Azure struct{}

func NewAzure() Adapter        { return Azure{} }
func (Azure) Provider() string { return "azure-devops" }
func (Azure) Initial(s Source, m string, c *Checkpoint) ([]Request, error) {
	return (readsource.Azure{}).Initial(s, m, c)
}
func (Azure) Decode(s Source, r Request, v Response) (PageResult, error) {
	p, e := (readsource.Azure{}).Decode(s, r, v)
	out := compatConvert[PageResult](p)
	out.Next = p.Next
	return out, e
}
