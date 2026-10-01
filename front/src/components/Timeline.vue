<template>
    <div class="timeline">
        <div class="d-flex align-center mb-2">
            <span class="font-weight-medium">Timeline</span>
            <span v-if="entries.length" class="caption grey--text ml-2">{{ entries.length }}</span>
            <v-spacer />
            <v-btn icon x-small :loading="loading" @click="load" title="Refresh"><v-icon small>mdi-refresh</v-icon></v-btn>
        </div>

        <v-alert v-if="error" color="error" icon="mdi-alert-octagon-outline" outlined text dense class="mb-2">
            {{ error }}
        </v-alert>

        <div v-if="showApprovals && approvals.length" class="approvals mb-2">
            <ApprovalCard v-for="a in approvals" :key="a.id" :a="a" :show-target="false" @decided="load" />
        </div>

        <div v-if="!loading && !entries.length" class="caption grey--text mb-2">No comments or actions yet.</div>

        <div v-for="e in entries" :key="e.id" class="entry d-flex" :class="{ action: e.kind === 'action' }">
            <v-avatar :size="e.kind === 'action' ? 22 : 28" :color="avatarColor(e)" class="mr-2 flex-shrink-0">
                <v-icon v-if="e.kind === 'action'" x-small dark>{{ actionIcon(e) }}</v-icon>
                <v-icon v-else-if="e.author_kind === 'agent'" small dark>mdi-robot-outline</v-icon>
                <span v-else class="white--text initials">{{ initials(e.author) }}</span>
            </v-avatar>

            <div class="flex-grow-1 min-width-0">
                <div class="d-flex align-center flex-wrap header">
                    <span class="font-weight-medium">{{ e.author || 'unknown' }}</span>
                    <v-chip v-if="e.author_kind === 'agent'" x-small label color="primary" outlined class="ml-1">agent</v-chip>
                    <v-chip v-else-if="e.author_kind === 'system'" x-small label outlined class="ml-1">system</v-chip>
                    <span v-if="e.kind === 'action'" class="ml-1">{{ actionText(e) }}</span>
                    <span class="caption grey--text ml-2" :title="$format.date(e.created_at, '{MMM} {DD}, {HH}:{mm}:{ss}')">
                        {{ $format.timeSinceNow(e.created_at) }} ago
                    </span>
                    <span v-if="e.edited_at" class="caption grey--text ml-1">(edited)</span>
                    <span v-if="e.meta && e.meta.via === 'mcp'" class="caption grey--text ml-1">via MCP</span>
                    <span v-if="e.meta && e.meta.approved_by" class="status-chip ok ml-1" title="Executed after a human approval">
                        <v-icon size="12">mdi-account-check-outline</v-icon> approved by {{ e.meta.approved_by }}
                    </span>
                    <v-spacer />
                    <template v-if="e.editable && editing !== e.id">
                        <v-btn v-if="e.kind === 'comment'" icon x-small @click="startEdit(e)" title="Edit"><v-icon x-small>mdi-pencil</v-icon></v-btn>
                        <v-btn icon x-small :loading="deleting === e.id" @click="remove(e)" title="Delete"><v-icon x-small>mdi-delete</v-icon></v-btn>
                    </template>
                </div>

                <div v-if="editing === e.id" class="mt-1">
                    <v-textarea v-model="editBody" outlined dense auto-grow rows="2" hide-details class="body-2" />
                    <div class="d-flex mt-1" style="gap: 8px">
                        <v-btn x-small color="primary" :disabled="!editBody.trim()" :loading="saving" @click="saveEdit(e)">Save</v-btn>
                        <v-btn x-small text @click="editing = null">Cancel</v-btn>
                    </div>
                </div>
                <Markdown v-else-if="e.body" :src="e.body" :widgets="[]" class="body" />
            </div>
        </div>

        <div class="mt-3">
            <v-textarea
                v-model="body"
                outlined
                dense
                auto-grow
                rows="2"
                hide-details
                placeholder="Add a comment (markdown supported)"
                class="body-2"
                @keydown.ctrl.enter="submit"
                @keydown.meta.enter="submit"
            />
            <div class="d-flex align-center mt-1">
                <span class="caption grey--text">Ctrl+Enter to send</span>
                <v-spacer />
                <v-btn small color="primary" :disabled="!body.trim()" :loading="posting" @click="submit">Comment</v-btn>
            </div>
        </div>
    </div>
</template>

<script>
import Markdown from '@/components/Markdown.vue';
import ApprovalCard from '@/components/ApprovalCard.vue';
import { agentActionName } from '@/utils/workflow';

const actionIcons = {
    resolved: 'mdi-check',
    suppressed: 'mdi-bell-off',
    reopened: 'mdi-restore',
    created: 'mdi-plus',
    updated: 'mdi-pencil',
    enabled: 'mdi-toggle-switch',
    disabled: 'mdi-toggle-switch-off',
    deleted: 'mdi-delete',
    acknowledged: 'mdi-hand-back-right-outline',
    assigned: 'mdi-account-arrow-left-outline',
    unassigned: 'mdi-account-remove-outline',
    mitigated: 'mdi-shield-check-outline',
    severity_changed: 'mdi-alert-outline',
    resolution_updated: 'mdi-pencil',
    auto_resolved: 'mdi-check-all',
    muted: 'mdi-wrench-clock',
    unmuted: 'mdi-bell-ring-outline',
    ended: 'mdi-stop',
    approval_requested: 'mdi-account-clock-outline',
    approval_approved: 'mdi-account-check-outline',
    approval_rejected: 'mdi-account-cancel-outline',
    approval_denied: 'mdi-cancel',
};

const targetNames = { incident: 'the incident', alert: 'the alert', alerting_rule: 'the rule', maintenance_window: 'the maintenance window' };

export default {
    components: { Markdown, ApprovalCard },

    props: {
        targetType: { type: String, required: true },
        targetId: { type: String, required: true },
        showApprovals: { type: Boolean, default: true },
    },

    data() {
        return {
            entries: [],
            approvals: [],
            loading: false,
            error: '',
            body: '',
            posting: false,
            editing: null,
            editBody: '',
            saving: false,
            deleting: null,
        };
    },

    watch: {
        targetId() {
            this.load();
        },
    },

    mounted() {
        this.load();
    },

    methods: {
        load() {
            this.loading = true;
            this.$api.getComments(this.targetType, this.targetId, (data, error) => {
                this.loading = false;
                if (error) {
                    this.error = error;
                    return;
                }
                this.error = '';
                this.entries = Array.isArray(data) ? data : [];
            });
            if (this.showApprovals) {
                this.$api.getApprovals({ status: 'pending', target_type: this.targetType, target_id: this.targetId }, (data, error) => {
                    this.approvals = !error && Array.isArray(data) ? data : [];
                });
            }
        },
        submit() {
            const body = this.body.trim();
            if (!body || this.posting) {
                return;
            }
            this.posting = true;
            this.$api.addComment(this.targetType, this.targetId, body, (data, error) => {
                this.posting = false;
                if (error) {
                    this.error = error;
                    return;
                }
                this.error = '';
                this.body = '';
                this.entries.push(data);
            });
        },
        startEdit(e) {
            this.editing = e.id;
            this.editBody = e.body;
        },
        saveEdit(e) {
            this.saving = true;
            this.$api.updateComment(e.id, this.editBody.trim(), (data, error) => {
                this.saving = false;
                if (error) {
                    this.error = error;
                    return;
                }
                this.error = '';
                this.editing = null;
                const i = this.entries.findIndex((x) => x.id === e.id);
                if (i >= 0) {
                    this.$set(this.entries, i, data);
                }
            });
        },
        remove(e) {
            if (!confirm('Delete this entry?')) {
                return;
            }
            this.deleting = e.id;
            this.$api.deleteComment(e.id, (data, error) => {
                this.deleting = null;
                if (error) {
                    this.error = error;
                    return;
                }
                this.error = '';
                this.entries = this.entries.filter((x) => x.id !== e.id);
            });
        },
        initials(name) {
            const parts = (name || '?')
                .trim()
                .split(/[\s._@-]+/)
                .filter(Boolean);
            return ((parts[0] || '?')[0] + (parts.length > 1 ? parts[1][0] : '')).toUpperCase();
        },
        avatarColor(e) {
            if (e.kind === 'action') {
                return 'grey';
            }
            return e.author_kind === 'agent' ? 'primary' : 'blue-grey';
        },
        actionIcon(e) {
            return actionIcons[(e.meta && e.meta.action) || ''] || 'mdi-information-variant';
        },
        actionText(e) {
            const m = e.meta || {};
            const requested = agentActionName(m.requested_action).toLowerCase();
            switch (m.action) {
                case 'assigned':
                    return `assigned ${targetNames[e.target_type]} to ${m.assignee}`;
                case 'severity_changed':
                    return `set the severity to ${m.severity}`;
                case 'resolution_updated':
                    return 'updated the resolution';
                case 'auto_resolved':
                    return 'resolved the incident automatically: the SLO is met again';
                case 'muted':
                    return 'muted notifications';
                case 'unmuted':
                    return 'sent the postponed notifications';
                case 'approval_requested':
                    return `asked for approval to ${requested} (#${m.approval_id})`;
                case 'approval_approved':
                    return `approved ${m.requested_by}'s request to ${requested} (#${m.approval_id})`;
                case 'approval_rejected':
                    return `rejected ${m.requested_by}'s request to ${requested} (#${m.approval_id})`;
                case 'approval_denied':
                    return `was denied by the project policy to ${requested}`;
            }
            const action = m.action || 'updated';
            let text = `${action} ${targetNames[e.target_type] || ''}`.trim();
            if (action === 'updated' && e.meta && e.meta.changed) {
                text += ` (${e.meta.changed.split(',').join(', ')})`;
            }
            return text;
        },
    },
};
</script>

<style scoped>
.entry {
    padding: 6px 0;
}
.approvals {
    display: flex;
    flex-direction: column;
    gap: 8px;
}
.entry.action {
    padding: 4px 0 4px 3px;
}
.entry .header {
    font-size: 14px;
    gap: 2px;
    min-height: 24px;
}
.initials {
    font-size: 11px;
    font-weight: 500;
}
.min-width-0 {
    min-width: 0;
}
.body {
    font-size: 14px;
}
.body:deep(p:last-child) {
    margin-bottom: 0;
}
</style>
