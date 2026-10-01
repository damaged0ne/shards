package api

import "github.com/coroot/coroot/model"

const mcpCheckDetailsMax = 20

// mcpCheckDetails returns the findings of a failing check (bounded to keep the response small).
func mcpCheckDetails(c *model.Check) []string {
	if c.Details == nil || c.Details.Len() == 0 {
		return nil
	}
	items := c.Details.Items()
	if len(items) > mcpCheckDetailsMax {
		items = append(items[:mcpCheckDetailsMax:mcpCheckDetailsMax], "...")
	}
	return items
}
