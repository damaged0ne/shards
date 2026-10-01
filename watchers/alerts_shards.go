package watchers

import "github.com/coroot/coroot/model"

// checkAlertSeverity raises the severity of a check-based alert to critical when the check itself
// reports a critical condition (e.g. a Kafka consumer group is stalled, an Elasticsearch cluster is red),
// so one rule per check covers both the warning and the critical states.
func checkAlertSeverity(severity model.Status, check *model.Check) model.Status {
	if check.Status == model.CRITICAL && severity < model.CRITICAL {
		return model.CRITICAL
	}
	return severity
}
