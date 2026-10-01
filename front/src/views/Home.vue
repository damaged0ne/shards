<template>
    <Views :loading="loading" :error="error">
        <div v-if="home" class="home">
            <div class="intro">
                <div>
                    <h1 class="title">{{ allClear ? 'All clear' : 'Requires attention' }}</h1>
                    <div class="sub">
                        <template v-if="allClear">No open incidents, firing alerts or pending approvals.</template>
                        <template v-else>
                            <span v-if="unacknowledged" class="danger-text"
                                >{{ unacknowledged }} unacknowledged {{ $pluralize('incident', unacknowledged) }}</span
                            >
                            <span v-if="unacknowledged && home.approvals.length"> · </span>
                            <span v-if="home.approvals.length"
                                >{{ home.approvals.length }} {{ $pluralize('action', home.approvals.length) }} waiting for approval</span
                            >
                        </template>
                    </div>
                </div>
                <div class="stats">
                    <router-link class="stat" :to="link('incidents')">
                        <span class="n" :class="{ 'is-danger': home.incidents.length }">{{ home.incidents.length }}</span>
                        <span class="l">open incidents</span>
                    </router-link>
                    <router-link class="stat" :to="link('alerts')">
                        <span class="n" :class="{ 'is-warning': home.alerts_total }">{{ home.alerts_total }}</span>
                        <span class="l">firing alerts</span>
                    </router-link>
                    <router-link class="stat" :to="link('alerts', 'approvals')">
                        <span class="n" :class="{ 'is-warning': home.approvals.length }">{{ home.approvals.length }}</span>
                        <span class="l">approvals</span>
                    </router-link>
                    <router-link class="stat" :to="link('alerts', 'maintenance')">
                        <span class="n">{{ home.maintenance.length }}</span>
                        <span class="l">in maintenance</span>
                    </router-link>
                </div>
            </div>

            <div class="grid">
                <div class="col">
                    <section class="panel">
                        <header>
                            <v-icon size="16" class="h-icon danger-text">mdi-alert-outline</v-icon>
                            <span>Open incidents</span>
                            <v-spacer />
                            <router-link :to="link('incidents')" class="more">All incidents</router-link>
                        </header>
                        <div v-if="!home.incidents.length" class="empty">No open incidents.</div>
                        <router-link v-for="i in home.incidents" :key="i.key" class="row" :to="incidentLink(i.key)">
                            <span class="sev" :class="i.severity" />
                            <span class="key mono">i-{{ i.key }}</span>
                            <span class="grow">
                                <span class="primary-text">{{ $utils.appId(i.application_id).name }}</span>
                                <span class="secondary-text">{{ i.short_description }}</span>
                            </span>
                            <span v-if="i.in_maintenance" class="status-chip info" title="Notifications muted by a maintenance window">
                                <v-icon size="12">mdi-wrench-clock</v-icon>
                            </span>
                            <span class="status-chip" :class="statusChip(i.status)">{{ statusName(i.status) }}</span>
                            <span class="assignee" :title="i.assignee ? 'Assignee' : ''">{{ i.assignee || 'unassigned' }}</span>
                            <span class="age">{{ $format.timeSinceNow(i.opened_at) }}</span>
                        </router-link>
                    </section>

                    <section class="panel">
                        <header>
                            <v-icon size="16" class="h-icon warning-text">mdi-bell-outline</v-icon>
                            <span>Firing alerts</span>
                            <span v-if="home.alerts_total > home.alerts.length" class="count"
                                >{{ home.alerts.length }} of {{ home.alerts_total }}</span
                            >
                            <v-spacer />
                            <router-link :to="link('alerts')" class="more">All alerts</router-link>
                        </header>
                        <div v-if="!home.alerts.length" class="empty">No critical or warning alerts.</div>
                        <router-link v-for="a in home.alerts" :key="a.id" class="row" :to="alertLink(a.id)">
                            <span class="status-chip" :class="a.severity === 'critical' ? 'critical' : 'warning'">{{ a.severity }}</span>
                            <span class="grow">
                                <span class="primary-text">{{ a.summary }}</span>
                                <span class="secondary-text">
                                    <template v-if="a.application_id">{{ $utils.appId(a.application_id).name }} · </template>{{ a.rule_name }}
                                </span>
                            </span>
                            <span v-if="a.in_maintenance" class="status-chip info" title="Notifications muted by a maintenance window">
                                <v-icon size="12">mdi-wrench-clock</v-icon> maintenance
                            </span>
                            <span class="age">{{ $format.timeSinceNow(a.opened_at) }}</span>
                        </router-link>
                    </section>
                </div>

                <div class="col">
                    <section class="panel">
                        <header>
                            <v-icon size="16" class="h-icon">mdi-account-check-outline</v-icon>
                            <span>Waiting for you</span>
                            <v-spacer />
                            <router-link :to="link('alerts', 'approvals')" class="more">Approvals</router-link>
                        </header>
                        <div v-if="!home.approvals.length" class="empty">No agent actions waiting for approval.</div>
                        <div v-else class="approvals">
                            <ApprovalCard v-for="a in home.approvals" :key="a.id" :a="a" compact @decided="get" />
                        </div>
                    </section>

                    <section class="panel">
                        <header>
                            <v-icon size="16" class="h-icon">mdi-wrench-clock</v-icon>
                            <span>Active maintenance</span>
                            <v-spacer />
                            <router-link :to="link('alerts', 'maintenance')" class="more">Maintenance</router-link>
                        </header>
                        <div v-if="!home.maintenance.length" class="empty">No active maintenance windows.</div>
                        <router-link v-for="w in home.maintenance" :key="w.id" class="row" :to="link('alerts', 'maintenance')">
                            <span class="status-chip warning">active</span>
                            <span class="grow">
                                <span class="primary-text">{{ w.name }}</span>
                                <span class="secondary-text">{{ scopeText(w.scope) }}</span>
                            </span>
                            <span class="age" :title="$format.date(w.current_to, '{MMM} {DD}, {HH}:{mm}')">
                                ends in {{ $format.durationPretty(Math.max(0, w.current_to - Date.now())) }}
                            </span>
                        </router-link>
                    </section>

                    <section class="panel">
                        <header>
                            <v-icon size="16" class="h-icon">mdi-robot-outline</v-icon>
                            <span>Recent agent activity</span>
                        </header>
                        <div v-if="!home.activity.length" class="empty">No agent activity yet.</div>
                        <router-link v-for="c in home.activity" :key="c.id" class="row activity" :to="targetLink(c)">
                            <span class="bot"><v-icon size="13">mdi-robot-outline</v-icon></span>
                            <span class="grow">
                                <span class="primary-text">
                                    <b>{{ c.author }}</b> {{ activityText(c) }}
                                    <span class="mono target">{{ targetText(c) }}</span>
                                </span>
                                <span v-if="c.body" class="secondary-text">{{ c.body }}</span>
                            </span>
                            <span class="age">{{ $format.timeSinceNow(c.created_at) }}</span>
                        </router-link>
                    </section>
                </div>
            </div>
        </div>
    </Views>
</template>

<script>
import Views from '@/views/Views.vue';
import ApprovalCard from '@/components/ApprovalCard.vue';
import { incidentStatusChip, incidentStatusName, maintenanceScopeText, targetRoute } from '@/utils/workflow';

export default {
    components: { Views, ApprovalCard },

    data() {
        return { home: null, loading: false, error: '' };
    },

    mounted() {
        this.get();
        this.$events.watch(this, this.get, 'refresh');
    },

    computed: {
        unacknowledged() {
            return this.home ? this.home.incidents.filter((i) => i.status === 'triggered').length : 0;
        },
        allClear() {
            const h = this.home;
            return h && !h.incidents.length && !h.alerts.length && !h.approvals.length;
        },
    },

    methods: {
        get() {
            this.loading = true;
            this.$api.getHome((data, error) => {
                this.loading = false;
                if (error) {
                    this.error = error;
                    return;
                }
                this.error = '';
                this.home = data;
            });
        },
        link(view, id) {
            return { name: 'overview', params: { view, id }, query: { ...this.$utils.contextQuery(), incident: undefined, alert: undefined } };
        },
        incidentLink(key) {
            return targetRoute('incident', key, this.$utils.contextQuery());
        },
        alertLink(id) {
            return targetRoute('alert', id, this.$utils.contextQuery());
        },
        targetLink(c) {
            return targetRoute(c.target_type, c.target_id, this.$utils.contextQuery()) || this.link('home');
        },
        statusChip: incidentStatusChip,
        statusName: incidentStatusName,
        scopeText: maintenanceScopeText,
        activityText(c) {
            if (c.kind === 'comment') {
                return 'commented on';
            }
            const action = ((c.meta && c.meta.action) || 'updated').replaceAll('_', ' ');
            return action === 'approval requested' ? 'asked for approval on' : action;
        },
        targetText(c) {
            switch (c.target_type) {
                case 'incident':
                    return 'i-' + c.target_id;
                case 'alerting_rule':
                    return c.target_title || c.target_id;
                case 'maintenance_window':
                    return (c.meta && c.meta.window) || 'maintenance window';
            }
            return c.target_type.replace('_', ' ') + ' ' + c.target_id;
        },
    },
};
</script>

<style scoped>
.home {
    max-width: 1400px;
}
.intro {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    justify-content: space-between;
    gap: 16px;
    margin-bottom: 16px;
}
.title {
    font-size: 1.35rem;
    margin: 0;
}
.sub {
    color: var(--text-2);
    font-size: 13.5px;
    margin-top: 2px;
}
.danger-text {
    color: var(--danger-11) !important;
}
.warning-text {
    color: var(--warning-11) !important;
}
.stats {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
}
.stat {
    display: flex;
    flex-direction: column;
    min-width: 96px;
    padding: 8px 12px;
    border: 1px solid var(--border);
    border-radius: var(--r-md);
    background: var(--surface);
    color: var(--text-1) !important;
}
.stat:hover {
    border-color: var(--border-strong);
    text-decoration: none !important;
}
.stat .n {
    font-size: 20px;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    line-height: 1.2;
}
.stat .n.is-danger {
    color: var(--danger-10);
}
.stat .n.is-warning {
    color: var(--warning-11);
}
.stat .l {
    font-size: 12px;
    color: var(--text-2);
}
.grid {
    display: grid;
    grid-template-columns: minmax(0, 3fr) minmax(0, 2fr);
    gap: 16px;
    align-items: start;
}
@media (max-width: 1100px) {
    .grid {
        grid-template-columns: minmax(0, 1fr);
    }
}
.col {
    display: flex;
    flex-direction: column;
    gap: 16px;
    min-width: 0;
}
.panel {
    border: 1px solid var(--border);
    border-radius: var(--r-lg);
    background: var(--surface);
    padding: 4px 0 6px;
    min-width: 0;
}
.panel header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 14px;
    font-weight: 600;
    font-size: 14px;
}
.h-icon {
    color: var(--text-2) !important;
}
.count {
    font-weight: 400;
    font-size: 12px;
    color: var(--text-2);
}
.more {
    font-size: 12.5px;
    font-weight: 500;
}
.empty {
    padding: 6px 14px 10px;
    font-size: 13px;
    color: var(--text-3);
}
.row {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 44px;
    padding: 6px 14px;
    color: var(--text-1) !important;
    border-top: 1px solid var(--border-soft);
}
.row:hover {
    background: var(--hover);
    text-decoration: none !important;
}
.sev {
    flex: none;
    width: 4px;
    align-self: stretch;
    border-radius: 2px;
    background: var(--gray-7);
}
.sev.critical {
    background: var(--danger-9);
}
.sev.warning {
    background: var(--warning-9);
}
.key {
    font-size: 12px;
    color: var(--text-2);
    flex: none;
}
.grow {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
}
.primary-text {
    font-size: 13.5px;
    font-weight: 500;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}
.secondary-text {
    font-size: 12.5px;
    color: var(--text-2);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}
.assignee {
    font-size: 12.5px;
    color: var(--text-2);
    flex: none;
    max-width: 120px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}
.age {
    flex: none;
    font-size: 12px;
    color: var(--text-3);
    font-variant-numeric: tabular-nums;
}
.approvals {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 4px 12px 8px;
}
.activity .primary-text {
    font-weight: 400;
}
.target {
    font-size: 12px;
    color: var(--text-2);
}
.bot {
    flex: none;
    width: 22px;
    height: 22px;
    border-radius: 50%;
    display: grid;
    place-items: center;
    background: var(--accent-4);
}
.bot .v-icon {
    color: var(--accent-11) !important;
}
@media (max-width: 600px) {
    .assignee,
    .key {
        display: none;
    }
}
</style>
