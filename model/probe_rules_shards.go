package model

import "github.com/coroot/coroot/timeseries"

// probeBuiltinAlertingRules are the built-in rules for the synthetic probe checks (shards fork).
func probeBuiltinAlertingRules() []AlertingRule {
	return []AlertingRule{
		probeRule("probe-down", "Probe is failing", Checks.ProbeDown.Id, CRITICAL, 0, 2*timeseries.Minute,
			"A synthetic probe failed several times in a row: the endpoint is unreachable, returns an unexpected status or response, or times out."),
		probeRule("probe-latency", "Slow probe response", Checks.ProbeLatency.Id, WARNING, 2*timeseries.Minute, 5*timeseries.Minute,
			"The response time measured by a synthetic probe exceeds the threshold."),
		probeRule("probe-tls-cert-expiry", "TLS certificate expires soon", Checks.ProbeTLSCertExpiry.Id, WARNING, 0, 30*timeseries.Minute,
			"The TLS certificate checked by a synthetic probe expires soon. Renew it before it expires."),
		probeRule("probe-tls-cert-expiry-critical", "TLS certificate is about to expire", Checks.ProbeTLSCertExpiryCritical.Id, CRITICAL, 0, 30*timeseries.Minute,
			"The TLS certificate checked by a synthetic probe expires in a few days. Clients will reject the connections once it expires."),
		probeRule("probe-tls-cert-invalid", "Invalid TLS certificate", Checks.ProbeTLSCertInvalid.Id, CRITICAL, 0, 5*timeseries.Minute,
			"The TLS certificate checked by a synthetic probe is expired, issued by an untrusted CA or doesn't match the hostname."),
	}
}

func probeRule(id AlertingRuleId, name string, check CheckId, severity Status, forDuration, keepFiringFor timeseries.Duration, description string) AlertingRule {
	return AlertingRule{
		Id:   id,
		Name: name,
		Source: AlertSource{
			Type:  AlertSourceTypeCheck,
			Check: &CheckSource{CheckId: check},
		},
		Selector:      AppSelector{Type: AppSelectorTypeAll},
		Severity:      severity,
		For:           forDuration,
		KeepFiringFor: keepFiringFor,
		Templates:     AlertTemplates{Description: description},
		Enabled:       true,
		Builtin:       true,
	}
}
