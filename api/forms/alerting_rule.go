package forms

import (
	"fmt"
	"strings"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
)

// AlertingRuleForm validates an alerting rule submitted via the REST API (UI) or the MCP tools (agents).
type AlertingRuleForm struct {
	model.AlertingRule
}

func (f *AlertingRuleForm) Valid() bool {
	return f.Validate() == nil
}

func (f *AlertingRuleForm) Validate() error {
	return ValidateAlertingRule(&f.AlertingRule)
}

func ValidateAlertingRule(r *model.AlertingRule) error {
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("name is required")
	}
	switch r.Severity {
	case model.WARNING, model.CRITICAL:
	default:
		return fmt.Errorf("severity must be 'warning' or 'critical'")
	}
	if r.For < 0 || r.KeepFiringFor < 0 {
		return fmt.Errorf("for and keep_firing_for must not be negative")
	}
	switch r.Source.Type {
	case model.AlertSourceTypeCheck:
		if r.Source.Check == nil || r.Source.Check.CheckId == "" {
			return fmt.Errorf("source.check.check_id is required for the check source")
		}
		if _, ok := model.GetCheckConfigs()[r.Source.Check.CheckId]; !ok {
			return fmt.Errorf("unknown check_id: %s", r.Source.Check.CheckId)
		}
	case model.AlertSourceTypePromQL:
		if r.Source.PromQL == nil || strings.TrimSpace(r.Source.PromQL.Expression) == "" {
			return fmt.Errorf("source.promql.expression is required for the promql source")
		}
		if err := validateCustomQuery(r.Source.PromQL.Expression); err != nil {
			return err
		}
	case model.AlertSourceTypeLogPatterns:
		if r.Source.LogPattern == nil || len(r.Source.LogPattern.Severities) == 0 {
			return fmt.Errorf("source.log_pattern.severities is required for the log_patterns source")
		}
		if r.Source.LogPattern.MinCount < 0 || r.Source.LogPattern.MaxAlertsPerApp < 0 {
			return fmt.Errorf("source.log_pattern counters must not be negative")
		}
	case model.AlertSourceTypeKubernetesEvents:
		if r.Source.KubernetesEvents == nil {
			return fmt.Errorf("source.kubernetes_events is required for the kubernetes_events source")
		}
		if r.Source.KubernetesEvents.MinCount < 0 || r.Source.KubernetesEvents.MaxAlertsPerApp < 0 {
			return fmt.Errorf("source.kubernetes_events counters must not be negative")
		}
	default:
		return fmt.Errorf("invalid source type: %q", r.Source.Type)
	}
	switch r.Selector.Type {
	case model.AppSelectorTypeAll, "":
	case model.AppSelectorTypeCategory:
		if len(r.Selector.Categories) == 0 {
			return fmt.Errorf("selector.categories is required for the category selector")
		}
	case model.AppSelectorTypeApplications:
		if len(r.Selector.ApplicationIdPatterns) == 0 {
			return fmt.Errorf("selector.application_id_patterns is required for the applications selector")
		}
	default:
		return fmt.Errorf("invalid selector type: %q", r.Selector.Type)
	}
	return nil
}

// CommentForm is a timeline comment posted by a user or an agent.
type CommentForm struct {
	TargetType string `json:"target_type"`
	TargetId   string `json:"target_id"`
	Body       string `json:"body"`
}

func (f *CommentForm) Valid() bool {
	return f.Validate() == nil
}

func (f *CommentForm) Validate() error {
	return ValidateCommentBody(f.Body)
}

func ValidateCommentBody(body string) error {
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("comment body is required")
	}
	if len(body) > db.CommentMaxBodyLength {
		return fmt.Errorf("comment body is too long (max %d bytes)", db.CommentMaxBodyLength)
	}
	return nil
}
