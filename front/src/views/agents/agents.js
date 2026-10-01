// shards fork: shared helpers of the Agents area.

export const scopes = [
    { value: 'read', name: 'read', description: 'Query only: incidents, alerts, apps, metrics, logs, traces.' },
    { value: 'triage', name: 'triage', description: 'read + comments and acknowledge-type actions.' },
    { value: 'operator', name: 'operator', description: 'triage + resolve / suppress / reopen alerts, edit alerting rules.' },
    { value: 'admin', name: 'admin', description: "Everything the owner's role allows." },
];

export const scopeClass = {
    read: 'info',
    triage: 'success',
    operator: 'warning',
    admin: 'danger',
};

export const events = [
    { value: 'incident_opened', name: 'Incident opened' },
    { value: 'incident_escalated', name: 'Incident escalated' },
    { value: 'alert_fired', name: 'Alert fired' },
    { value: 'mention', name: '@mention in a comment' },
    { value: 'approval_decided', name: 'Approval decided' },
];

export const statusText = {
    active: 'active',
    idle: 'idle',
    never: 'never connected',
    expired: 'expired',
    disabled: 'disabled',
};

export function mcpUrl(coroot) {
    return `${window.location.origin}${coroot.base_path}mcp`;
}

export function randomSecret() {
    const bytes = new Uint8Array(24);
    window.crypto.getRandomValues(bytes);
    return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
}
