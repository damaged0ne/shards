<template>
    <div class="timeline">
        <div class="d-flex align-center mb-2">
            <span class="font-weight-medium">Timeline</span>
            <span v-if="entries.length" class="caption grey--text ml-2">{{ entries.length }}</span>
            <v-spacer />
            <v-btn v-if="askable" x-small outlined class="mr-1" @click="ask = true" title="Dispatch a task to an operator agent">
                <v-icon x-small left>mdi-robot-outline</v-icon>Ask agent
            </v-btn>
            <v-btn icon x-small :loading="loading" @click="load" title="Refresh"><v-icon small>mdi-refresh</v-icon></v-btn>
        </div>
        <AskAgentDialog v-if="askable" v-model="ask" :target-type="targetType" :target-id="targetId" @sent="load" />

        <v-alert v-if="error" color="error" icon="mdi-alert-octagon-outline" outlined text dense class="mb-2">
            {{ error }}
        </v-alert>

        <div v-if="!loading && !entries.length" class="caption grey--text mb-2">No comments or actions yet.</div>

        <div v-for="e in entries" :key="e.id" class="entry d-flex" :class="{ action: e.kind === 'action' }">
            <v-avatar :size="e.kind === 'action' ? 22 : 28" :color="avatarColor(e)" class="mr-2 flex-shrink-0">
                <v-icon v-if="e.kind === 'action'" x-small dark>{{ actionIcon(e) }}</v-icon>
                <v-icon v-else-if="e.author_kind === 'agent'" small dark>mdi-robot-outline</v-icon>
                <span v-else class="white--text initials">{{ initials(e.author) }}</span>
            </v-avatar>

            <div class="flex-grow-1 min-width-0">
                <div class="d-flex align-center flex-wrap header">
                    <router-link v-if="agentLink(e)" :to="agentLink(e)" class="font-weight-medium">{{ e.author }}</router-link>
                    <span v-else class="font-weight-medium">{{ e.author || 'unknown' }}</span>
                    <v-chip v-if="e.author_kind === 'agent'" x-small label color="primary" outlined class="ml-1">agent</v-chip>
                    <v-chip v-else-if="e.author_kind === 'system'" x-small label outlined class="ml-1">system</v-chip>
                    <span v-if="e.kind === 'action'" class="ml-1">{{ actionText(e) }}</span>
                    <router-link v-if="askedAgentLink(e)" :to="askedAgentLink(e)" class="ml-1 mono">@{{ e.meta.agent }}</router-link>
                    <span class="caption grey--text ml-2" :title="$format.date(e.created_at, '{MMM} {DD}, {HH}:{mm}:{ss}')">
                        {{ $format.timeSinceNow(e.created_at) }} ago
                    </span>
                    <span v-if="e.edited_at" class="caption grey--text ml-1">(edited)</span>
                    <span v-if="e.meta && e.meta.via === 'mcp'" class="caption grey--text ml-1">via MCP</span>
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
                <span class="caption grey--text">Ctrl+Enter to send · @agent-name wakes an agent up</span>
                <v-spacer />
                <v-btn small color="primary" :disabled="!body.trim()" :loading="posting" @click="submit">Comment</v-btn>
            </div>
        </div>
    </div>
</template>

<script>
import Markdown from '@/components/Markdown.vue';
import AskAgentDialog from '@/components/AskAgentDialog.vue';

const actionIcons = {
    resolved: 'mdi-check',
    suppressed: 'mdi-bell-off',
    reopened: 'mdi-restore',
    created: 'mdi-plus',
    updated: 'mdi-pencil',
    enabled: 'mdi-toggle-switch',
    disabled: 'mdi-toggle-switch-off',
    deleted: 'mdi-delete',
    asked_agent: 'mdi-robot-outline',
};

const targetNames = { incident: 'the incident', alert: 'the alert', alerting_rule: 'the rule' };

export default {
    components: { Markdown, AskAgentDialog },

    props: {
        targetType: { type: String, required: true },
        targetId: { type: String, required: true },
    },

    data() {
        return {
            entries: [],
            loading: false,
            error: '',
            body: '',
            posting: false,
            editing: null,
            editBody: '',
            saving: false,
            deleting: null,
            ask: false,
        };
    },

    computed: {
        askable() {
            return this.targetType === 'incident' || this.targetType === 'alert';
        },
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
        agentLink(e) {
            // shards fork: registered agents link to their page in the Agents area
            if (e.author_kind !== 'agent' || !e.meta || !e.meta.agent_id) {
                return null;
            }
            return { name: 'overview', params: { view: 'agents', id: e.meta.agent_id }, query: this.$utils.contextQuery() };
        },
        askedAgentLink(e) {
            if (!e.meta || e.meta.action !== 'asked_agent' || !e.meta.agent_id) {
                return null;
            }
            return { name: 'overview', params: { view: 'agents', id: e.meta.agent_id }, query: this.$utils.contextQuery() };
        },
        actionText(e) {
            const action = (e.meta && e.meta.action) || 'updated';
            if (action === 'asked_agent') {
                return 'asked an agent to look at ' + (targetNames[e.target_type] || 'this');
            }
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
