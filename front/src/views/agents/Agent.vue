<template>
    <Views :loading="loading" :error="error">
        <template #subtitle>
            <span class="mono">{{ agent ? agent.name : id }}</span>
        </template>

        <template v-if="agent">
            <div class="card hero mb-4">
                <div class="d-flex align-center flex-wrap" style="gap: 10px">
                    <span class="dot" :class="agent.status" />
                    <span class="title-name mono">{{ agent.name }}</span>
                    <Chip :tone="scopeClass[agent.scope]">{{ agent.scope }}</Chip>
                    <Chip :tone="statusClass">{{ statusText[agent.status] }}</Chip>
                    <v-spacer />
                    <template v-if="editable">
                        <v-btn small outlined @click="form = true"><v-icon small left>mdi-pencil</v-icon>Edit</v-btn>
                        <v-btn small outlined color="error" :loading="deleting" @click="remove"><v-icon small left>mdi-delete</v-icon>Delete</v-btn>
                    </template>
                </div>
                <div v-if="agent.description" class="desc mt-2">{{ agent.description }}</div>
                <dl class="kv mt-3">
                    <dt>Kind / model</dt>
                    <dd>{{ [agent.vendor, agent.model].filter(Boolean).join(' · ') || '—' }}</dd>
                    <dt>Owner</dt>
                    <dd>{{ agent.owner || '—' }} <span class="grey--text">(scope is capped by the owner's role)</span></dd>
                    <dt>Projects</dt>
                    <dd>{{ allowedProjects }}</dd>
                    <dt>Expires</dt>
                    <dd>{{ agent.expires_at ? $format.date(agent.expires_at, '{MMM} {DD}, {YYYY}') : 'never' }}</dd>
                    <dt>Last hour</dt>
                    <dd>
                        {{ stats.calls_last_hour }} calls ·
                        <span :class="{ 'red--text': stats.errors_last_hour }">{{ stats.errors_last_hour }} errors</span> ·
                        <span :class="{ 'red--text': stats.denied_last_hour }">{{ stats.denied_last_hour }} denied</span>
                    </dd>
                </dl>
            </div>

            <div class="layout-grid">
                <section class="card">
                    <div class="card-head">
                        <h3>Activity</h3>
                        <span class="caption grey--text ml-2">tool calls and REST writes, newest first · kept 30 days</span>
                        <v-spacer />
                        <v-btn-toggle v-model="activityFilter" dense mandatory class="seg">
                            <v-btn x-small value="all">all</v-btn>
                            <v-btn x-small value="problems">errors</v-btn>
                        </v-btn-toggle>
                    </div>
                    <div class="card-body">
                        <div v-if="!filteredActivity.length" class="grey--text caption">No calls recorded yet.</div>
                        <div class="tl">
                            <div v-for="a in filteredActivity" :key="a.id" class="tl-item">
                                <span class="tl-dot" :class="a.status" />
                                <div class="tl-row">
                                    <span class="chan">{{ a.channel }}</span>
                                    <b class="mono tool">{{ a.tool }}</b>
                                    <Chip :tone="callClass[a.status]">{{ a.status }}</Chip>
                                    <router-link v-if="targetLink(a)" :to="targetLink(a)" class="caption mono"
                                        >{{ a.target_type }} {{ a.target_id }}</router-link
                                    >
                                    <span v-else-if="a.target_id" class="caption mono grey--text">{{ a.target_type }} {{ a.target_id }}</span>
                                    <span class="meta">
                                        {{ a.duration_ms }} ms ·
                                        <span :title="$format.date(a.time, '{MMM} {DD}, {HH}:{mm}:{ss}')"
                                            >{{ $format.timeSinceNow(a.time) }} ago</span
                                        >
                                    </span>
                                </div>
                                <div v-if="a.args" class="tl-args" :title="a.args">{{ a.args }}</div>
                                <div v-if="a.error" class="tl-error">{{ a.error }}</div>
                            </div>
                        </div>
                        <div v-if="moreActivity" class="mt-2">
                            <v-btn x-small text :loading="loadingMore" @click="loadMore">Load older</v-btn>
                        </div>
                    </div>
                </section>

                <div class="side">
                    <section class="card">
                        <div class="card-head">
                            <h3>Keys</h3>
                            <v-spacer />
                            <v-btn v-if="editable" x-small outlined :loading="creatingKey" @click="createKey">Create key</v-btn>
                        </div>
                        <div class="card-body">
                            <div v-if="newKey" class="new-key mb-3">
                                <div class="caption mb-1">New key — copy it now, it won't be shown again:</div>
                                <div class="d-flex align-center">
                                    <code class="flex-grow-1">{{ newKey }}</code>
                                    <CopyButton :text="newKey" />
                                </div>
                            </div>
                            <div v-for="k in agent.keys" :key="k.id" class="key-row">
                                <v-icon small>mdi-key-outline</v-icon>
                                <span class="mono">{{ k.description }}</span>
                                <v-spacer />
                                <v-btn v-if="editable" x-small text color="error" @click="revokeKey(k)">revoke</v-btn>
                            </div>
                            <div v-if="!agent.keys.length" class="caption grey--text">
                                No keys yet. Create one and configure the agent's MCP client with it.
                            </div>
                        </div>
                    </section>

                    <section class="card">
                        <div class="card-head">
                            <h3>Dispatch</h3>
                            <v-spacer />
                            <v-btn v-if="editable && dispatch.url" x-small outlined :loading="testing" @click="test">Send test</v-btn>
                        </div>
                        <div class="card-body">
                            <template v-if="dispatch.enabled">
                                <div class="mono caption url">{{ dispatch.url }}</div>
                                <div class="caption mt-1">
                                    <Chip v-for="e in dispatch.events || []" :key="e" class="mr-1 mb-1">{{ eventName(e) }}</Chip>
                                </div>
                                <div class="caption grey--text mt-1">
                                    min severity {{ dispatch.min_severity || 'warning' }} · {{ dispatch.rate_limit_per_hour || 30 }}/h · dedup
                                    {{ dispatch.dedup_minutes || 30 }}m · signing secret {{ agent.secret_set ? 'set' : 'not set' }}
                                </div>
                            </template>
                            <div v-else class="caption grey--text">
                                Not configured: the agent has to poll. <a v-if="editable" @click="form = true">Configure</a>
                            </div>
                            <div v-if="testResult" class="caption mt-2" :class="testResult.status === 'delivered' ? 'green--text' : 'red--text'">
                                test: {{ testResult.status }}
                                <template v-if="testResult.response_code">(HTTP {{ testResult.response_code }})</template>
                                {{ testResult.error }}
                            </div>
                        </div>
                    </section>

                    <section class="card">
                        <div class="card-head"><h3>Sessions</h3></div>
                        <div class="card-body">
                            <div v-for="s in sessions" :key="s.session_id" class="session">
                                <div class="d-flex align-center" style="gap: 6px">
                                    <span class="tl-dot static" :class="sessionActive(s) ? 'ok' : 'idle'" />
                                    <span class="font-weight-medium">{{ s.client_name || 'unknown client' }}</span>
                                    <span v-if="s.client_version" class="mono caption">{{ s.client_version }}</span>
                                    <v-spacer />
                                    <span class="caption grey--text">{{ s.calls }} calls</span>
                                </div>
                                <div class="caption grey--text">
                                    {{ $format.date(s.started_at, '{MMM} {DD}, {HH}:{mm}') }} → last seen {{ $format.timeSinceNow(s.last_seen) }} ago
                                </div>
                            </div>
                            <div v-if="!sessions.length" class="caption grey--text">No MCP sessions yet.</div>
                        </div>
                    </section>
                </div>
            </div>

            <section class="card mt-4">
                <div class="card-head">
                    <h3>Dispatch deliveries</h3>
                    <span class="caption grey--text ml-2">retried with backoff up to 6 attempts</span>
                </div>
                <div class="card-body pa-0">
                    <table class="dl-table">
                        <thead>
                            <tr>
                                <th>Event</th>
                                <th>Status</th>
                                <th class="hide-sm">Target</th>
                                <th class="r">Attempts</th>
                                <th>Created</th>
                                <th></th>
                            </tr>
                        </thead>
                        <tbody>
                            <template v-for="d in deliveries">
                                <tr :key="d.id">
                                    <td class="mono">{{ d.event }}</td>
                                    <td>
                                        <Chip :tone="deliveryClass[d.status]">{{ d.status }}</Chip>
                                        <span v-if="d.response_code" class="caption grey--text ml-1">HTTP {{ d.response_code }}</span>
                                        <div v-if="d.error" class="caption red--text">{{ d.error }}</div>
                                    </td>
                                    <td class="hide-sm mono caption">{{ d.dedup_key }}</td>
                                    <td class="r num">{{ d.attempts }}</td>
                                    <td class="text-no-wrap caption">{{ $format.timeSinceNow(d.created_at) }} ago</td>
                                    <td class="r">
                                        <v-btn x-small text @click="toggle(d.id)">{{ expanded === d.id ? 'hide' : 'payload' }}</v-btn>
                                    </td>
                                </tr>
                                <tr v-if="expanded === d.id" :key="d.id + '-p'">
                                    <td colspan="6">
                                        <pre class="payload">{{ pretty(d.payload) }}</pre>
                                    </td>
                                </tr>
                            </template>
                            <tr v-if="!deliveries.length">
                                <td colspan="6" class="caption grey--text">No deliveries yet.</td>
                            </tr>
                        </tbody>
                    </table>
                </div>
            </section>

            <ConnectAgent class="mt-4" :api-key="newKey" :closable="false" />

            <AgentForm v-model="form" :agent="agent" :users="users" @saved="get" />
        </template>
    </Views>
</template>

<script>
import Views from '@/views/Views.vue';
import CopyButton from '@/components/CopyButton.vue';
import AgentForm from './AgentForm.vue';
import ConnectAgent from './ConnectAgent.vue';
import Chip from './Chip.vue';
import { scopeClass, statusText, events } from './agents';

export default {
    inject: { shell: { default: null } },
    components: { Views, CopyButton, AgentForm, ConnectAgent, Chip },

    props: {
        id: String,
    },

    data() {
        return {
            agent: null,
            editable: false,
            sessions: [],
            activity: [],
            deliveries: [],
            users: [],
            loading: false,
            loadingMore: false,
            moreActivity: false,
            error: '',
            form: false,
            deleting: false,
            creatingKey: false,
            newKey: '',
            testing: false,
            testResult: null,
            expanded: null,
            activityFilter: 'all',
        };
    },

    computed: {
        scopeClass() {
            return scopeClass;
        },
        statusText() {
            return statusText;
        },
        callClass() {
            return { ok: 'success', error: 'danger', denied: 'warning' };
        },
        deliveryClass() {
            return { delivered: 'success', pending: 'info', failed: 'danger', skipped: 'neutral' };
        },
        statusClass() {
            return { active: 'success', idle: 'warning', expired: 'danger', disabled: 'danger' }[this.agent.status] || 'neutral';
        },
        stats() {
            return this.agent.stats || { calls_last_hour: 0, errors_last_hour: 0, denied_last_hour: 0 };
        },
        dispatch() {
            return this.agent.dispatch || {};
        },
        allowedProjects() {
            const ids = this.agent.allowed_projects || [];
            if (!ids.length) {
                return 'all projects the owner can access';
            }
            const projects = (this.shell && this.shell.projects) || [];
            return ids.map((id) => (projects.find((p) => p.id === id) || { name: id }).name).join(', ');
        },
        filteredActivity() {
            if (this.activityFilter === 'problems') {
                return this.activity.filter((a) => a.status !== 'ok');
            }
            return this.activity;
        },
    },

    watch: {
        id() {
            this.newKey = '';
            this.get();
        },
    },

    mounted() {
        this.get();
        this.$events.watch(this, this.get, 'refresh');
    },

    methods: {
        get() {
            this.loading = true;
            this.$api.getAgent(this.id, (data, error) => {
                this.loading = false;
                if (error) {
                    this.error = error;
                    return;
                }
                this.error = '';
                this.agent = data.agent;
                this.editable = data.editable;
                this.sessions = data.sessions || [];
                this.activity = data.activity || [];
                this.moreActivity = this.activity.length >= 100;
                this.deliveries = data.deliveries || [];
            });
            if (!this.users.length) {
                this.$api.getAgents((data) => {
                    this.users = (data && data.users) || [];
                });
            }
        },
        loadMore() {
            const last = this.activity[this.activity.length - 1];
            this.loadingMore = true;
            this.$api.get(this.$api.projectPath(`agents/${this.id}/activity`), { before: last.id, limit: 100 }, (data, error) => {
                this.loadingMore = false;
                if (error) {
                    this.error = error;
                    return;
                }
                this.activity = this.activity.concat(data || []);
                this.moreActivity = (data || []).length >= 100;
            });
        },
        remove() {
            if (!confirm(`Delete agent ${this.agent.name}? Its keys are revoked.`)) {
                return;
            }
            this.deleting = true;
            this.$api.deleteAgent(this.agent.id, (data, error) => {
                this.deleting = false;
                if (error) {
                    this.error = error;
                    return;
                }
                this.$router
                    .push({ name: 'overview', params: { view: 'agents', id: undefined }, query: this.$utils.contextQuery() })
                    .catch((err) => err);
            });
        },
        createKey() {
            this.creatingKey = true;
            const description = `${this.agent.name}-${new Date().toISOString().slice(0, 10)}-${Math.random().toString(36).slice(2, 6)}`;
            this.$api.agentAction(this.agent.id, 'keys', { action: 'create', description }, (data, error) => {
                this.creatingKey = false;
                if (error) {
                    this.error = error;
                    return;
                }
                this.newKey = data.key;
                this.get();
            });
        },
        revokeKey(k) {
            if (!confirm(`Revoke key ${k.description}? Agents using it lose access immediately.`)) {
                return;
            }
            this.$api.agentAction(this.agent.id, 'keys', { action: 'revoke', key_id: k.id }, (data, error) => {
                if (error) {
                    this.error = error;
                    return;
                }
                this.get();
            });
        },
        test() {
            this.testing = true;
            this.testResult = null;
            this.$api.agentAction(this.agent.id, 'test', {}, (data, error) => {
                this.testing = false;
                if (error) {
                    this.testResult = { status: 'failed', error };
                    return;
                }
                this.testResult = data;
                this.get();
            });
        },
        eventName(e) {
            const ev = events.find((x) => x.value === e);
            return ev ? ev.name : e;
        },
        sessionActive(s) {
            return Date.now() - s.last_seen < 5 * 60 * 1000;
        },
        targetLink(a) {
            const q = this.$utils.contextQuery();
            if (a.target_type === 'incident' && a.target_id) {
                return { name: 'overview', params: { view: 'incidents', id: undefined }, query: { ...q, incident: a.target_id } };
            }
            if (a.target_type === 'alert' && a.target_id && !a.target_id.includes(',')) {
                return { name: 'overview', params: { view: 'alerts', id: undefined }, query: { ...q, alert: a.target_id } };
            }
            if (a.target_type === 'application' && a.target_id) {
                return { name: 'overview', params: { view: 'applications', id: a.target_id }, query: q };
            }
            return null;
        },
        toggle(id) {
            this.expanded = this.expanded === id ? null : id;
        },
        pretty(payload) {
            try {
                return JSON.stringify(JSON.parse(payload), null, 2);
            } catch {
                return payload;
            }
        },
    },
};
</script>

<style scoped>
.card {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--r-md);
    min-width: 0;
}
.hero {
    padding: 14px 16px;
}
.title-name {
    font-size: 18px;
    font-weight: 600;
}
.desc {
    color: var(--text-2);
}
.kv {
    display: grid;
    grid-template-columns: 110px 1fr;
    gap: 4px 12px;
    font-size: 13px;
}
.kv dt {
    color: var(--text-2);
}
.kv dd {
    margin: 0;
}
.layout-grid {
    display: grid;
    grid-template-columns: minmax(0, 1.6fr) minmax(280px, 1fr);
    gap: 16px;
    align-items: start;
}
@media (max-width: 960px) {
    .layout-grid {
        grid-template-columns: minmax(0, 1fr);
    }
}
.side {
    display: flex;
    flex-direction: column;
    gap: 16px;
    min-width: 0;
}
.card-head {
    display: flex;
    align-items: center;
    padding: 10px 14px;
    border-bottom: 1px solid var(--border-soft);
    gap: 4px;
    flex-wrap: wrap;
}
.card-head h3 {
    font-size: 13.5px;
    font-weight: 600;
}
.card-body {
    padding: 12px 14px;
}
.seg {
    background: transparent !important;
}
.seg .v-btn {
    text-transform: none;
    letter-spacing: 0;
}
.dot {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--gray-7);
}
.dot.active {
    background: var(--success-9);
    box-shadow: 0 0 0 3px var(--success-4);
}
.dot.idle {
    background: var(--warning-8);
}
.dot.expired,
.dot.disabled {
    background: var(--danger-8);
}

/* tool-call timeline (prototype .timeline / .tl-*) */
.tl {
    position: relative;
    padding-left: 18px;
}
.tl::before {
    content: '';
    position: absolute;
    left: 5px;
    top: 6px;
    bottom: 6px;
    width: 1px;
    background: var(--border-strong);
}
.tl-item {
    position: relative;
    padding: 6px 0 10px;
}
.tl-dot {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--gray-7);
    box-shadow: 0 0 0 2px var(--surface);
    display: inline-block;
}
.tl-item .tl-dot {
    position: absolute;
    left: -17px;
    top: 11px;
}
.tl-dot.ok {
    background: var(--success-9);
}
.tl-dot.error {
    background: var(--danger-9);
}
.tl-dot.denied,
.tl-dot.idle {
    background: var(--warning-8);
}
.tl-row {
    display: flex;
    gap: 8px;
    align-items: center;
    flex-wrap: wrap;
}
.tl-row .tool {
    font-size: 12.5px;
}
.tl-row .meta {
    margin-left: auto;
    font-size: 11.5px;
    color: var(--text-3);
    white-space: nowrap;
}
.chan {
    font-size: 10.5px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--text-3);
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 0 5px;
    line-height: 16px;
}
.tl-args {
    font-family: var(--mono);
    font-size: 11px;
    color: var(--text-2);
    margin-top: 3px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}
.tl-error {
    font-size: 12px;
    color: var(--danger-11);
    margin-top: 2px;
}
.key-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 4px 0;
    font-size: 13px;
}
.new-key {
    background: var(--success-2);
    border: 1px solid var(--success-6);
    border-radius: var(--r-sm);
    padding: 8px 10px;
}
.new-key code {
    background: none !important;
    word-break: break-all;
}
.url {
    word-break: break-all;
}
.session + .session {
    margin-top: 10px;
}
.dl-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
}
.dl-table th {
    text-align: left;
    font-weight: 500;
    font-size: 11.5px;
    color: var(--text-2);
    padding: 8px 14px;
    border-bottom: 1px solid var(--border);
}
.dl-table td {
    padding: 6px 14px;
    border-bottom: 1px solid var(--border-soft);
    vertical-align: top;
}
.dl-table .r {
    text-align: right;
}
.payload {
    font-family: var(--mono);
    font-size: 11.5px;
    background: var(--surface-sunk);
    border-radius: var(--r-sm);
    padding: 8px 10px;
    max-height: 360px;
    overflow: auto;
    white-space: pre-wrap;
    word-break: break-all;
}
@media (max-width: 700px) {
    .hide-sm {
        display: none;
    }
}
</style>
