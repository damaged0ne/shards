<template>
    <div class="workflow">
        <div v-if="wf" class="bar">
            <div class="facts">
                <Chip :tone="statusChip">{{ statusName }}</Chip>
                <span v-if="wf.status === 'resolved'" class="fact">
                    {{ wf.resolved_kind === 'auto' ? 'automatically (SLO is met again)' : 'by ' + wf.resolved_by }}
                </span>
                <Chip v-if="wf.severity_override" :tone="wf.severity_override === 'critical' ? 'danger' : 'warning'" title="Severity set manually">
                    {{ wf.severity_override }} (manual)
                </Chip>
                <Chip v-if="wf.maintenance" tone="info" :title="`Notifications muted by the maintenance window '${wf.maintenance.window_name}'`">
                    <v-icon size="12">mdi-wrench-clock</v-icon> maintenance: {{ wf.maintenance.window_name }}
                </Chip>
                <span class="fact">
                    <span class="k">Assignee</span>
                    <template v-if="wf.assignee">
                        <v-icon v-if="wf.assignee_kind === 'agent'" size="13">mdi-robot-outline</v-icon>
                        {{ wf.assignee }}
                    </template>
                    <span v-else class="muted">unassigned</span>
                </span>
                <span v-if="wf.acknowledged_by" class="fact">
                    <span class="k">Acknowledged</span> {{ wf.acknowledged_by }}, {{ $format.timeSinceNow(wf.acknowledged_at) }} ago
                </span>
                <span v-if="wf.mitigated_by" class="fact">
                    <span class="k">Mitigated</span> {{ wf.mitigated_by }}, {{ $format.timeSinceNow(wf.mitigated_at) }} ago
                </span>
            </div>
            <div class="buttons">
                <v-btn
                    v-if="wf.open && !wf.acknowledged_at"
                    small
                    color="primary"
                    depressed
                    :loading="busy === 'acknowledge'"
                    @click="act('acknowledge')"
                >
                    <v-icon small left>mdi-hand-back-right-outline</v-icon>Acknowledge
                </v-btn>
                <v-btn v-if="!isMine" small outlined :loading="busy === 'assign'" @click="act('assign')">
                    <v-icon small left>mdi-account-arrow-left-outline</v-icon>Assign to me
                </v-btn>
                <v-btn v-if="wf.open && wf.status !== 'mitigated'" small outlined :loading="busy === 'mitigate'" @click="act('mitigate')">
                    <v-icon small left>mdi-shield-check-outline</v-icon>Mark mitigated
                </v-btn>
                <v-btn small :color="wf.open ? 'primary' : undefined" :outlined="!wf.open" :depressed="wf.open" @click="openResolve">
                    <v-icon small left>mdi-check-circle-outline</v-icon
                    >{{ wf.open ? 'Resolve' : wf.resolution ? 'Edit resolution' : 'Add resolution' }}
                </v-btn>
                <v-btn small outlined @click="openPostmortem"><v-icon small left>mdi-file-document-outline</v-icon>Postmortem</v-btn>
                <v-menu offset-y left>
                    <template #activator="{ on, attrs }">
                        <v-btn icon small v-bind="attrs" v-on="on" title="More"><v-icon small>mdi-dots-vertical</v-icon></v-btn>
                    </template>
                    <v-list dense>
                        <v-subheader>Severity</v-subheader>
                        <v-list-item v-for="s in ['critical', 'warning', '']" :key="s" @click="act('set_severity', { severity: s })">
                            <v-icon small class="mr-2">{{
                                (wf.severity_override || '') === s ? 'mdi-radiobox-marked' : 'mdi-radiobox-blank'
                            }}</v-icon>
                            {{ s || 'automatic (from the SLO check)' }}
                        </v-list-item>
                        <v-divider class="my-1" />
                        <v-list-item :disabled="!wf.assignee" @click="act('unassign')"
                            ><v-icon small class="mr-2">mdi-account-remove-outline</v-icon>Unassign</v-list-item
                        >
                    </v-list>
                </v-menu>
            </div>
        </div>

        <v-alert v-if="error" color="error" outlined text dense class="mt-2 mb-0">{{ error }}</v-alert>
        <v-alert v-if="notice" color="info" outlined text dense dismissible class="mt-2 mb-0" @input="notice = ''">{{ notice }}</v-alert>

        <div v-if="wf && wf.pending_approvals && wf.pending_approvals.length" class="mt-3 approvals">
            <ApprovalCard v-for="a in wf.pending_approvals" :key="a.id" :a="a" :show-target="false" @decided="decided" />
        </div>

        <div v-if="wf && (wf.resolution || wf.root_cause || (wf.follow_ups && wf.follow_ups.length))" class="resolution mt-3">
            <div v-if="wf.resolution">
                <div class="k">Resolution</div>
                <Markdown :src="wf.resolution" :widgets="[]" class="md" />
            </div>
            <div v-if="wf.root_cause">
                <div class="k">Root cause</div>
                <Markdown :src="wf.root_cause" :widgets="[]" class="md" />
            </div>
            <div v-if="wf.follow_ups && wf.follow_ups.length">
                <div class="k">Follow-ups</div>
                <ul class="md">
                    <li v-for="(f, i) in wf.follow_ups" :key="i">{{ f }}</li>
                </ul>
            </div>
        </div>

        <v-dialog v-model="resolve.active" max-width="640">
            <v-card>
                <v-card-title>{{ wf && wf.open ? 'Resolve incident' : 'Resolution' }}</v-card-title>
                <v-card-text>
                    <div v-if="wf && wf.open" class="caption mb-3">
                        Resolving closes the incident. If the SLO is still violated, a new incident will be opened after a 30-minute cooldown.
                    </div>
                    <v-textarea
                        v-model="resolve.resolution"
                        label="What fixed it? (required)"
                        outlined
                        dense
                        auto-grow
                        rows="3"
                        :rules="[$validators.notEmpty]"
                    />
                    <v-textarea v-model="resolve.root_cause" label="Root cause" outlined dense auto-grow rows="2" />
                    <v-textarea v-model="resolve.follow_ups" label="Follow-up items (one per line)" outlined dense auto-grow rows="2" hide-details />
                </v-card-text>
                <v-card-actions class="px-6 pb-4">
                    <v-spacer />
                    <v-btn text @click="resolve.active = false">Cancel</v-btn>
                    <v-btn color="primary" depressed :disabled="!resolve.resolution.trim()" :loading="busy === 'resolve'" @click="submitResolve">
                        {{ wf && wf.open ? 'Resolve' : 'Save' }}
                    </v-btn>
                </v-card-actions>
            </v-card>
        </v-dialog>

        <v-dialog v-model="postmortem.active" max-width="900" scrollable>
            <v-card>
                <v-card-title class="d-flex">
                    Postmortem draft
                    <v-spacer />
                    <v-btn-toggle v-model="postmortem.raw" dense mandatory class="mr-2">
                        <v-btn :value="false" small>Preview</v-btn>
                        <v-btn :value="true" small>Markdown</v-btn>
                    </v-btn-toggle>
                    <CopyButton :text="postmortem.markdown" :disabled="!postmortem.markdown" title="Copy markdown" />
                    <v-btn icon small @click="postmortem.active = false"><v-icon small>mdi-close</v-icon></v-btn>
                </v-card-title>
                <v-card-text class="postmortem">
                    <v-progress-linear v-if="postmortem.loading" indeterminate height="2" />
                    <v-alert v-if="postmortem.error" color="error" outlined text dense>{{ postmortem.error }}</v-alert>
                    <pre v-if="postmortem.raw" class="raw">{{ postmortem.markdown }}</pre>
                    <Markdown v-else-if="postmortem.markdown" :src="postmortem.markdown" :widgets="[]" class="md" />
                </v-card-text>
            </v-card>
        </v-dialog>
    </div>
</template>

<script>
import Chip from '@/views/agents/Chip.vue';
import ApprovalCard from '@/components/ApprovalCard.vue';
import Markdown from '@/components/Markdown.vue';
import CopyButton from '@/components/CopyButton.vue';
import { incidentStatusChip, incidentStatusName } from '@/utils/workflow';

export default {
    components: { Chip, ApprovalCard, Markdown, CopyButton },

    inject: ['shell'],

    props: {
        incidentKey: { type: String, required: true },
    },

    data() {
        return {
            wf: null,
            busy: '',
            error: '',
            notice: '',
            resolve: { active: false, resolution: '', root_cause: '', follow_ups: '' },
            postmortem: { active: false, loading: false, error: '', markdown: '', raw: false },
        };
    },

    computed: {
        statusChip() {
            return incidentStatusChip(this.wf.status);
        },
        statusName() {
            return incidentStatusName(this.wf.status);
        },
        isMine() {
            const user = this.shell && this.shell.user;
            return user && this.wf.assignee && this.wf.assignee === user.name;
        },
    },

    watch: {
        incidentKey() {
            this.get();
        },
    },

    mounted() {
        this.get();
        this.$events.watch(this, this.get, 'refresh');
    },

    methods: {
        get() {
            this.$api.incidentWorkflow(this.incidentKey, null, (data, error) => {
                if (error) {
                    this.error = error;
                    return;
                }
                this.error = '';
                this.wf = data;
            });
        },
        act(action, extra) {
            this.busy = action;
            this.error = '';
            this.$api.incidentWorkflow(this.incidentKey, { action, ...(extra || {}) }, (data, error, status) => {
                this.busy = '';
                if (error) {
                    this.error = error;
                    return;
                }
                if (status === 202) {
                    this.notice = `Waiting for approval (#${data.approval_id}).`;
                    this.get();
                    return;
                }
                this.wf = data;
                this.$emit('changed');
            });
        },
        decided() {
            this.get();
            this.$emit('changed');
        },
        openResolve() {
            this.resolve = {
                active: true,
                resolution: this.wf.resolution || '',
                root_cause: this.wf.root_cause || '',
                follow_ups: (this.wf.follow_ups || []).join('\n'),
            };
        },
        submitResolve() {
            const follow_ups = this.resolve.follow_ups
                .split('\n')
                .map((s) => s.trim())
                .filter(Boolean);
            this.act('resolve', { resolution: this.resolve.resolution.trim(), root_cause: this.resolve.root_cause.trim(), follow_ups });
            this.resolve.active = false;
        },
        openPostmortem() {
            this.postmortem = { active: true, loading: true, error: '', markdown: '', raw: false };
            this.$api.getIncidentPostmortem(this.incidentKey, (data, error) => {
                this.postmortem.loading = false;
                if (error) {
                    this.postmortem.error = error;
                    return;
                }
                this.postmortem.markdown = data.markdown;
            });
        },
    },
};
</script>

<style scoped>
.bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px 16px;
}
.facts {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px 14px;
    flex: 1;
    min-width: 0;
    font-size: 13.5px;
}
.fact .v-icon {
    color: var(--text-2) !important;
}
.k {
    font-weight: 600;
    color: var(--text-2);
    font-size: 12.5px;
    margin-right: 4px;
}
.muted {
    color: var(--text-3);
}
.buttons {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
}
.approvals {
    display: flex;
    flex-direction: column;
    gap: 8px;
}
.resolution {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
    gap: 12px;
    border-top: 1px solid var(--border-soft);
    padding-top: 10px;
}
.md {
    font-size: 13.5px;
}
.md:deep(p:last-child) {
    margin-bottom: 0;
}
.postmortem {
    min-height: 200px;
}
.raw {
    white-space: pre-wrap;
    font-family: var(--mono);
    font-size: 12.5px;
    background: var(--surface-sunk);
    border: 1px solid var(--border-soft);
    border-radius: var(--r-sm);
    padding: 12px;
}
</style>
