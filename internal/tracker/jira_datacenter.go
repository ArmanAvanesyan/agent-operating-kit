package tracker

import (
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
)

type JiraDataCenter struct{}

func (JiraDataCenter) Provider() string { return "jira-datacenter" }
func (JiraDataCenter) Initial(s Source, m string, c *Checkpoint) ([]Request, error) {
	return (readsource.JiraDataCenter{}).Initial(s, m, c)
}
func (JiraDataCenter) Decode(s Source, r Request, v Response) (PageResult, error) {
	p, e := (readsource.JiraDataCenter{}).Decode(s, r, v)
	out := compatConvert[PageResult](p)
	out.Next = p.Next
	return out, e
}
