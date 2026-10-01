// shards fork: shared helpers for the incident workflow, maintenance windows and agent approvals.

export const incidentStatuses = {
    triggered: { name: 'Triggered', chip: 'critical' },
    acknowledged: { name: 'Acknowledged', chip: 'warning' },
    mitigated: { name: 'Mitigated', chip: 'info' },
    resolved: { name: 'Resolved', chip: 'ok' },
};

export function incidentStatusChip(status) {
    return (incidentStatuses[status] || {}).chip || '';
}

export function incidentStatusName(status) {
    return (incidentStatuses[status] || {}).name || status;
}

export const agentActions = {
    delete_alerting_rule: 'Delete an alerting rule',
    disable_alerting_rule: 'Disable an alerting rule',
    update_alerting_rule: 'Update an alerting rule',
    suppress_alerts: 'Suppress alerts',
    resolve_alerts: 'Resolve alerts',
    create_maintenance_window: 'Create a maintenance window',
    end_maintenance_window: 'End a maintenance window',
    resolve_incident: 'Resolve an incident',
};

export function agentActionName(action) {
    return agentActions[action] || (action || '').replaceAll('_', ' ');
}

export const approvalStatuses = {
    pending: 'warning',
    approved: 'info',
    executed: 'ok',
    failed: 'danger',
    rejected: '',
};

const weekdays = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

export function weekdayNames() {
    return weekdays;
}

export function maintenanceScopeText(scope) {
    const s = scope || {};
    const parts = [];
    if ((s.application_patterns || []).length) {
        parts.push('apps: ' + s.application_patterns.join(', '));
    }
    if ((s.categories || []).length) {
        parts.push('categories: ' + s.categories.join(', '));
    }
    if ((s.node_patterns || []).length) {
        parts.push('nodes: ' + s.node_patterns.join(', '));
    }
    if ((s.alerting_rule_ids || []).length) {
        parts.push('rules: ' + s.alerting_rule_ids.join(', '));
    }
    return parts.length ? parts.join(' · ') : 'everything';
}

export function maintenanceScheduleText(w, format) {
    if (w.recurrence) {
        const r = w.recurrence;
        const days = (r.weekdays || []).map((d) => weekdays[d]).join(', ');
        return `weekly ${days} at ${r.start_time} ${r.timezone || 'UTC'} for ${r.duration_minutes}m`;
    }
    return `${format.date(w.starts_at, '{MMM} {DD}, {HH}:{mm}')} – ${format.date(w.ends_at, '{MMM} {DD}, {HH}:{mm}')}`;
}

export function maintenanceStatusChip(status) {
    return { active: 'warning', scheduled: 'info', ended: '', expired: '' }[status] || '';
}

// targetRoute returns the page of a timeline target (incident, alert, alerting rule, maintenance window).
export function targetRoute(type, id, query) {
    switch (type) {
        case 'incident':
            return { name: 'overview', params: { view: 'incidents' }, query: { ...query, incident: id } };
        case 'alert':
            return { name: 'overview', params: { view: 'alerts', id: undefined }, query: { ...query, alert: id } };
        case 'alerting_rule':
            return { name: 'overview', params: { view: 'alerts', id: 'rules' }, query: { ...query, rule: id } };
        case 'maintenance_window':
            return { name: 'overview', params: { view: 'alerts', id: 'maintenance' }, query };
    }
    return null;
}
