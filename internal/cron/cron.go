package cron

import (
	"fmt"

	"github.com/robfig/cron/v3"
)

// New returns a cron scheduler configured with second-level precision
// and panic recovery on each job. Call Start() on the returned instance.
func New() *cron.Cron {
	return cron.New(
		cron.WithSeconds(),
		cron.WithChain(cron.Recover(cron.DefaultLogger)),
	)
}

// PrintEntries logs all registered entries – useful during startup.
func PrintEntries(c *cron.Cron) {
	for _, e := range c.Entries() {
		fmt.Printf("[CRON] registered entry id=%d next=%s\n", e.ID, e.Next)
	}
}
