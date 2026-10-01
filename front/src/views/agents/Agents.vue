<template>
    <Views :loading="loading" :error="error">
        <div class="head">
            <div class="intro">
                Operator agents (Claude Code, custom bots, …) connect over MCP or the REST API with a scoped key. Every call is audited; agents can be
                woken up by signed webhooks instead of polling.
            </div>
            <div class="actions">
                <v-btn small outlined @click="connect = !connect"><v-icon small left>mdi-connection</v-icon>Connect an agent</v-btn>
                <v-btn v-if="editable" small color="primary" @click="openForm"><v-icon small left>mdi-plus</v-icon>New agent</v-btn>
            </div>
        </div>

        <ConnectAgent v-if="connect" class="mb-4" @close="connect = false" />

        <div class="stats mb-4">
            <div class="stat">
                <div class="label">Agents</div>
                <div class="value num">{{ agents.length }}</div>
                <div class="sub">{{ scopeSummary }}</div>
            </div>
            <div class="stat">
                <div class="label">Active now</div>
                <div class="value num">{{ activeCount }}</div>
                <div class="sub">seen in the last 5 minutes</div>
            </div>
            <div class="stat">
                <div class="label">Calls · last hour</div>
                <div class="value num">{{ totals.calls }}</div>
                <div class="sub">MCP tool calls and REST writes</div>
            </div>
            <div class="stat">
                <div class="label">Errors · denied</div>
                <div class="value num" :class="{ danger: totals.errors + totals.denied > 0 }">{{ totals.errors }} · {{ totals.denied }}</div>
                <div class="sub">last hour</div>
            </div>
        </div>

        <div class="card">
            <table class="agents-table">
                <thead>
                    <tr>
                        <th>Agent</th>
                        <th>Scope</th>
                        <th class="hide-sm">Owner</th>
                        <th class="r">Calls 1h</th>
                        <th class="r">Errors</th>
                        <th class="hide-sm">Last seen</th>
                        <th class="hide-sm">Dispatch</th>
                    </tr>
                </thead>
                <tbody>
                    <tr v-for="a in agents" :key="a.id">
                        <td>
                            <div class="d-flex align-center" style="gap: 8px">
                                <span class="dot" :class="a.status" :title="statusText[a.status]" />
                                <router-link :to="link(a)" class="name mono">{{ a.name }}</router-link>
                            </div>
                            <div class="caption meta">
                                <span v-if="a.vendor || a.model">{{ [a.vendor, a.model].filter(Boolean).join(' · ') }}</span>
                                <span v-else>{{ statusText[a.status] }}</span>
                            </div>
                        </td>
                        <td>
                            <Chip :tone="scopeClass[a.scope]">{{ a.scope }}</Chip>
                        </td>
                        <td class="hide-sm">{{ a.owner || '—' }}</td>
                        <td class="r num">{{ (a.stats && a.stats.calls_last_hour) || 0 }}</td>
                        <td class="r num">
                            <span :class="{ 'red--text': errors(a) }">{{ errors(a) }}</span>
                        </td>
                        <td class="hide-sm text-no-wrap">
                            <template v-if="a.stats && a.stats.last_seen">{{ $format.timeSinceNow(a.stats.last_seen) }} ago</template>
                            <span v-else class="grey--text">never</span>
                        </td>
                        <td class="hide-sm">
                            <Chip v-if="a.dispatch && a.dispatch.enabled" tone="success"><v-icon x-small>mdi-webhook</v-icon>webhook</Chip>
                            <span v-else class="grey--text">—</span>
                        </td>
                    </tr>
                    <tr v-if="!agents.length && !loading">
                        <td colspan="7" class="empty">
                            No agents registered yet.
                            <template v-if="editable">
                                <a @click="openForm">Register an agent</a> to give it a scoped key, an audit trail and a dispatch webhook.
                            </template>
                        </td>
                    </tr>
                </tbody>
            </table>
        </div>

        <AgentForm v-model="form" :users="users" :default-owner-id="defaultOwnerId" @saved="saved" />
    </Views>
</template>

<script>
import Views from '@/views/Views.vue';
import AgentForm from './AgentForm.vue';
import ConnectAgent from './ConnectAgent.vue';
import Chip from './Chip.vue';
import { scopeClass, statusText } from './agents';

export default {
    components: { Views, AgentForm, ConnectAgent, Chip },

    data() {
        return {
            agents: [],
            users: [],
            editable: false,
            defaultOwnerId: 0,
            loading: false,
            error: '',
            form: false,
            connect: false,
        };
    },

    computed: {
        scopeClass() {
            return scopeClass;
        },
        statusText() {
            return statusText;
        },
        activeCount() {
            return this.agents.filter((a) => a.status === 'active').length;
        },
        totals() {
            const t = { calls: 0, errors: 0, denied: 0 };
            this.agents.forEach((a) => {
                if (a.stats) {
                    t.calls += a.stats.calls_last_hour;
                    t.errors += a.stats.errors_last_hour;
                    t.denied += a.stats.denied_last_hour;
                }
            });
            return t;
        },
        scopeSummary() {
            const counts = {};
            this.agents.forEach((a) => (counts[a.scope] = (counts[a.scope] || 0) + 1));
            const s = Object.entries(counts).map(([k, v]) => `${v} ${k}`);
            return s.length ? s.join(' · ') : 'none yet';
        },
    },

    mounted() {
        this.get();
        this.$events.watch(this, this.get, 'refresh');
    },

    methods: {
        get() {
            this.loading = true;
            this.$api.getAgents((data, error) => {
                this.loading = false;
                if (error) {
                    this.error = error;
                    return;
                }
                this.error = '';
                this.agents = data.agents || [];
                this.users = data.users || [];
                this.editable = data.editable;
                this.defaultOwnerId = data.default_owner_id;
                if (!this.agents.length && this.$route.query.connect === undefined) {
                    this.connect = true;
                }
            });
        },
        errors(a) {
            return a.stats ? a.stats.errors_last_hour + a.stats.denied_last_hour : 0;
        },
        link(a) {
            return { name: 'overview', params: { view: 'agents', id: String(a.id) }, query: this.$utils.contextQuery() };
        },
        openForm() {
            this.form = true;
        },
        saved(agent) {
            this.$router.push(this.link(agent)).catch((err) => err);
        },
    },
};
</script>

<style scoped>
.head {
    display: flex;
    align-items: flex-start;
    gap: 16px;
    flex-wrap: wrap;
    margin-bottom: 16px;
}
.intro {
    flex: 1 1 360px;
    color: var(--text-2);
    font-size: 13px;
    max-width: 760px;
}
.actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
}
.stats {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(190px, 1fr));
    gap: 12px;
}
.stat,
.card {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--r-md);
}
.stat {
    padding: 12px 14px;
}
.stat .label,
.stat .sub {
    color: var(--text-2);
    font-size: 11.5px;
}
.stat .value {
    font-size: 22px;
    font-weight: 600;
    line-height: 1.4;
}
.stat .value.danger {
    color: var(--danger-11);
}
.card {
    overflow-x: auto;
}
.agents-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
}
.agents-table th {
    text-align: left;
    font-weight: 500;
    font-size: 11.5px;
    color: var(--text-2);
    padding: 8px 12px;
    border-bottom: 1px solid var(--border);
    white-space: nowrap;
}
.agents-table td {
    padding: 8px 12px;
    border-bottom: 1px solid var(--border-soft);
    vertical-align: middle;
}
.agents-table tr:last-child td {
    border-bottom: none;
}
.agents-table .r {
    text-align: right;
}
.agents-table .name {
    font-weight: 500;
}
.agents-table .meta {
    color: var(--text-3);
    padding-left: 16px;
}
.empty {
    text-align: center;
    color: var(--text-2);
    padding: 24px 12px !important;
}
.dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
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
@media (max-width: 700px) {
    .hide-sm {
        display: none;
    }
}
</style>
