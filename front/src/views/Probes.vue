<template>
    <Views :loading="loading" :error="error">
        <div class="d-flex align-center flex-wrap gap-2 mb-4">
            <v-text-field v-model="search" label="search" clearable dense hide-details prepend-inner-icon="mdi-magnify" outlined class="search" />
            <div class="summary">
                <span v-for="s in summary" :key="s.status" class="status-chip" :class="s.class">{{ s.count }} {{ s.status }}</span>
            </div>
            <v-spacer />
            <v-btn v-if="editable" color="primary" small @click="open('new')">
                <v-icon small class="mr-1">mdi-plus</v-icon>
                Add probe
            </v-btn>
            <v-menu offset-y max-width="460" :close-on-content-click="false">
                <template #activator="{ on }">
                    <v-icon v-on="on" color="primary" title="What are probes?">mdi-help-circle-outline</v-icon>
                </template>
                <v-card class="pa-3">
                    <div class="body-2">
                        Probes check HTTP endpoints, TCP ports, TLS certificates and DNS records from the shards server. The results are stored as
                        <code>shards_probe_*</code> metrics. Built-in alerts fire when a probe fails twice in a row, responds slower than 2s, or its
                        certificate expires in less than 14 days or is invalid.
                        <a :href="$utils.docsUrl('uptime/probes')" target="_blank">Learn more</a>
                    </div>
                </v-card>
            </v-menu>
        </div>

        <v-alert v-if="multicluster" color="info" outlined text> Probes are defined in the member projects of a multi-cluster project. </v-alert>

        <v-data-table
            sort-by="name"
            dense
            class="table"
            mobile-breakpoint="0"
            :items-per-page="50"
            :items="items"
            item-key="id"
            :headers="headers"
            :no-data-text="editable ? 'No probes yet: add one to monitor an endpoint' : 'No probes configured'"
            :footer-props="{ itemsPerPageOptions: [20, 50, 100, -1] }"
        >
            <template #item.name="{ item }">
                <div class="name">
                    <a @click="open(editable ? 'edit' : 'view', item)">{{ item.name }}</a>
                    <span class="type">{{ item.spec.type }}</span>
                </div>
                <div class="target" :title="item.spec.target">{{ item.spec.target }}</div>
            </template>

            <template #item.status="{ item }">
                <span class="status-chip" :class="statusClass(item.status)" :title="item.last_run_at ? 'last run ' + since(item.last_run_at) : ''">
                    {{ item.status }}
                </span>
            </template>

            <template #item.uptime="{ item }">
                <div class="d-flex align-center gap-2">
                    <span v-if="item.uptime !== null" class="num" :class="uptimeClass(item.uptime)">{{ percent(item.uptime) }}</span>
                    <span v-else class="grey--text">–</span>
                    <div v-if="item.up_chart" class="bars" :title="'availability within the time range'">
                        <span v-for="(v, i) in bars(item.up_chart)" :key="i" class="bar" :class="v" />
                    </div>
                </div>
            </template>

            <template #item.latency_p95="{ item }">
                <span v-if="item.latency_p95 !== null" class="num">{{ latency(item.latency_p95) }}</span>
                <span v-else class="grey--text">–</span>
            </template>

            <template #item.response="{ item }">
                <v-sparkline
                    v-if="item.latency_chart && item.latency_chart.some((v) => v !== null)"
                    :value="item.latency_chart.map((v) => (v === null ? 0 : v))"
                    smooth
                    fill
                    padding="2"
                    height="28"
                    color="blue lighten-2"
                    style="width: 96px"
                />
            </template>

            <template #item.cert_days_left="{ item }">
                <template v-if="item.cert_days_left !== null">
                    <span class="status-chip" :class="certClass(item)" :title="certTitle(item)">{{ certText(item) }}</span>
                </template>
                <span v-else class="grey--text">–</span>
            </template>

            <template #item.application="{ item }">
                <router-link
                    v-if="item.application_id"
                    :to="{
                        name: 'overview',
                        params: { view: 'applications', id: item.application_id, report: 'Uptime' },
                        query: $utils.contextQuery(),
                    }"
                    class="app"
                    :class="{ 'grey--text': !item.linked }"
                    :title="item.linked ? 'linked application' : 'the application created for this probe'"
                >
                    {{ appName(item.application_id) }}
                </router-link>
            </template>

            <template #item.last_error="{ item }">
                <span v-if="item.last_error" class="error-text" :title="item.last_error">{{ item.last_error }}</span>
            </template>

            <template #item.actions="{ item }">
                <div v-if="editable" class="d-flex">
                    <v-btn icon small title="Edit" @click="open('edit', item)"><v-icon small>mdi-pencil</v-icon></v-btn>
                    <v-btn icon small title="Delete" @click="open('delete', item)"><v-icon small>mdi-trash-can-outline</v-icon></v-btn>
                </div>
            </template>
        </v-data-table>

        <ProbeForm v-if="action" v-model="action" :key="formKey" :probe="probe" :applications="applications" :readonly="!editable" />
    </Views>
</template>

<script>
import Views from '@/views/Views.vue';
import ProbeForm, { latency } from '@/components/ProbeForm.vue';

export default {
    components: { Views, ProbeForm },

    data() {
        return {
            loading: false,
            error: '',
            search: '',
            probes: [],
            applications: [],
            editable: false,
            multicluster: false,
            action: '',
            probe: null,
            formKey: 0,
        };
    },

    computed: {
        headers() {
            return [
                { value: 'name', text: 'Probe' },
                { value: 'status', text: 'Status' },
                { value: 'uptime', text: 'Uptime' },
                { value: 'latency_p95', text: 'p95 latency' },
                { value: 'response', text: 'Response time', sortable: false },
                { value: 'cert_days_left', text: 'Certificate' },
                { value: 'application', text: 'Application', sortable: false },
                { value: 'last_error', text: 'Last error', sortable: false },
                { value: 'actions', text: '', sortable: false, width: '72px' },
            ].filter((h) => h.value !== 'actions' || this.editable);
        },
        items() {
            const q = (this.search || '').toLowerCase();
            if (!q) {
                return this.probes;
            }
            return this.probes.filter((p) => (p.name + ' ' + p.spec.target + ' ' + p.spec.type + ' ' + p.status).toLowerCase().includes(q));
        },
        summary() {
            const counts = {};
            this.probes.forEach((p) => (counts[p.status] = (counts[p.status] || 0) + 1));
            return ['down', 'up', 'unknown', 'paused']
                .filter((s) => counts[s])
                .map((s) => ({ status: s, count: counts[s], class: this.statusClass(s) }));
        },
    },

    mounted() {
        this.get();
        this.$events.watch(this, this.get, 'refresh');
    },

    methods: {
        get() {
            this.loading = true;
            this.error = '';
            this.$api.getProbes((data, error) => {
                this.loading = false;
                if (error) {
                    this.error = error;
                    return;
                }
                this.probes = data.probes || [];
                this.applications = data.applications || [];
                this.editable = data.editable;
                this.multicluster = data.multicluster;
            });
        },
        open(action, probe) {
            this.probe = probe || null;
            this.action = action;
            this.formKey++;
        },
        statusClass(status) {
            switch (status) {
                case 'up':
                    return 'ok';
                case 'down':
                    return 'critical';
            }
            return '';
        },
        uptimeClass(u) {
            if (u < 95) {
                return 'red--text';
            }
            if (u < 99.9) {
                return 'orange--text';
            }
            return '';
        },
        percent(u) {
            if (u >= 100) {
                return '100%';
            }
            return (u >= 99 ? u.toFixed(2) : u.toFixed(1)) + '%';
        },
        latency(v) {
            return latency(v);
        },
        bars(chart) {
            // compresses the availability series into up to 20 bars: ok / failed / no data
            const n = Math.min(20, chart.length);
            const size = chart.length / n;
            const res = [];
            for (let i = 0; i < n; i++) {
                const vs = chart.slice(Math.floor(i * size), Math.floor((i + 1) * size)).filter((v) => v !== null);
                if (!vs.length) {
                    res.push('none');
                } else if (vs.every((v) => v > 0)) {
                    res.push('up');
                } else if (vs.every((v) => v === 0)) {
                    res.push('down');
                } else {
                    res.push('partial');
                }
            }
            return res;
        },
        certText(item) {
            const d = item.cert_days_left;
            if (item.cert_valid === false) {
                return 'invalid';
            }
            if (d < 0) {
                return 'expired';
            }
            return Math.floor(d) + 'd left';
        },
        certClass(item) {
            const d = item.cert_days_left;
            if (item.cert_valid === false || d < 3) {
                return 'critical';
            }
            if (d < 14) {
                return 'warning';
            }
            return 'ok';
        },
        certTitle(item) {
            return [
                item.cert_subject,
                item.cert_issuer ? 'issuer: ' + item.cert_issuer : '',
                item.cert_not_after ? 'expires: ' + item.cert_not_after : '',
            ]
                .filter(Boolean)
                .join('\n');
        },
        appName(id) {
            const parts = (id || '').split(':');
            return parts[parts.length - 1];
        },
        since(t) {
            return this.$format.timeSinceNow(t * 1000) + ' ago';
        },
    },
};
</script>

<style scoped>
.search {
    max-width: 260px;
}
.summary {
    display: flex;
    gap: 6px;
}
.table:deep(th),
.table:deep(td) {
    padding: 6px 8px !important;
}
.table:deep(tr:hover) {
    background-color: unset !important;
}
.name {
    display: flex;
    align-items: center;
    gap: 6px;
    font-weight: 500;
}
.type {
    font-family: var(--mono);
    font-size: 10.5px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--text-2);
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 0 4px;
}
.target {
    font-family: var(--mono);
    font-size: 12px;
    color: var(--text-2);
    max-width: 32ch;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}
.num {
    font-variant-numeric: tabular-nums;
}
.bars {
    display: flex;
    gap: 1px;
    align-items: flex-end;
    height: 16px;
}
.bar {
    width: 3px;
    height: 100%;
    border-radius: 1px;
    background: var(--gray-5);
}
.bar.up {
    background: var(--success-9);
}
.bar.down {
    background: var(--danger-9);
}
.bar.partial {
    background: var(--warning-9);
}
.app {
    white-space: nowrap;
}
.error-text {
    display: inline-block;
    max-width: 26ch;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 12.5px;
    color: var(--danger-11);
}
</style>
