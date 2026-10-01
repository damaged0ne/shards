<template>
    <v-dialog v-model="dialog" max-width="760">
        <v-card class="pa-5">
            <div class="d-flex align-center font-weight-medium mb-4">
                <div>{{ title }}</div>
                <v-spacer />
                <v-btn icon @click="dialog = false"><v-icon>mdi-close</v-icon></v-btn>
            </div>
            <v-form ref="form" v-model="valid" :disabled="readonly || action === 'delete'">
                <template v-if="action === 'delete'">
                    <div class="mb-4">
                        Delete the probe <b>{{ form.name }}</b
                        >? Its metrics stay in the metrics storage until they expire.
                    </div>
                </template>
                <template v-else>
                    <div class="grid">
                        <div>
                            <div class="subtitle-1">Name</div>
                            <v-text-field v-model="form.name" outlined dense :rules="[$validators.notEmpty, isName]" placeholder="api-health" />
                        </div>
                        <div>
                            <div class="subtitle-1">Type</div>
                            <v-select v-model="form.spec.type" :items="types" outlined dense :menu-props="{ offsetY: true }" />
                        </div>
                    </div>

                    <div class="subtitle-1">Target</div>
                    <v-text-field v-model="form.spec.target" outlined dense :rules="[$validators.notEmpty]" :placeholder="targetPlaceholder" />

                    <div class="grid">
                        <div>
                            <div class="subtitle-1">Interval</div>
                            <v-text-field v-model="form.spec.interval" outlined dense placeholder="60s" hint="10s..1h" persistent-hint />
                        </div>
                        <div>
                            <div class="subtitle-1">Timeout</div>
                            <v-text-field v-model="form.spec.timeout" outlined dense placeholder="10s" />
                        </div>
                    </div>

                    <template v-if="form.spec.type === 'http'">
                        <div class="grid">
                            <div>
                                <div class="subtitle-1">Method</div>
                                <v-select v-model="form.spec.method" :items="methods" outlined dense :menu-props="{ offsetY: true }" />
                            </div>
                            <div>
                                <div class="subtitle-1">Expected status</div>
                                <v-text-field v-model="form.spec.expected_status" outlined dense placeholder="200-399" />
                            </div>
                        </div>
                        <div class="subtitle-1">Response body must contain</div>
                        <v-text-field v-model="form.spec.body_contains" outlined dense placeholder="optional substring, e.g. ok" />

                        <div class="subtitle-1">Request headers</div>
                        <div v-for="(h, i) in form.spec.headers" :key="i" class="d-flex gap-2 mb-2">
                            <v-text-field v-model="h.key" outlined dense hide-details placeholder="Header" />
                            <v-text-field v-model="h.value" outlined dense hide-details placeholder="Value" />
                            <v-btn icon small @click="form.spec.headers.splice(i, 1)"><v-icon small>mdi-trash-can-outline</v-icon></v-btn>
                        </div>
                        <v-btn small text color="primary" class="mb-2 px-1" @click="form.spec.headers.push({ key: '', value: '' })">
                            <v-icon small class="mr-1">mdi-plus</v-icon>Add header
                        </v-btn>
                        <v-checkbox v-model="form.spec.follow_redirects" label="Follow redirects" dense hide-details class="mt-0" />
                    </template>

                    <v-checkbox
                        v-if="form.spec.type === 'http' || form.spec.type === 'tls'"
                        v-model="form.spec.tls_skip_verify"
                        label="Skip TLS certificate verification (the expiry is still checked)"
                        dense
                        hide-details
                    />

                    <template v-if="form.spec.type === 'dns'">
                        <div class="grid">
                            <div>
                                <div class="subtitle-1">Record type</div>
                                <v-select v-model="form.spec.dns_record_type" :items="dnsTypes" outlined dense :menu-props="{ offsetY: true }" />
                            </div>
                            <div>
                                <div class="subtitle-1">DNS server</div>
                                <v-text-field v-model="form.spec.dns_server" outlined dense placeholder="system resolver" />
                            </div>
                        </div>
                    </template>

                    <div class="subtitle-1 mt-3">Application</div>
                    <div class="caption grey--text mb-1">
                        The results appear in the Uptime report of the application and the probe alerts fire for it. Unlinked probes get an
                        application of their own.
                    </div>
                    <v-autocomplete
                        v-model="form.spec.application_id"
                        :items="appItems"
                        outlined
                        dense
                        clearable
                        placeholder="not linked"
                        :menu-props="{ offsetY: true }"
                    />
                    <v-checkbox v-model="form.spec.paused" label="Paused" dense hide-details class="mt-0" />
                </template>

                <div v-if="result" class="result mt-4">
                    <div class="d-flex align-center gap-2 mb-1">
                        <span class="status-chip" :class="result.up ? 'ok' : 'critical'">{{ result.up ? 'up' : 'down' }}</span>
                        <span v-if="result.status_code" class="caption">HTTP {{ result.status_code }}</span>
                        <span class="caption grey--text">{{ phases }}</span>
                    </div>
                    <div v-if="result.error" class="caption red--text">{{ result.error }}</div>
                    <div v-if="result.cert" class="caption">
                        Certificate: {{ result.cert.subject }} (issuer: {{ result.cert.issuer }}), expires {{ certExpiry }}
                        <span v-if="result.cert.verified && !result.cert.valid" class="red--text">— invalid</span>
                    </div>
                    <div v-if="result.answers" class="caption">Answers: {{ result.answers.join(', ') }}</div>
                </div>

                <v-alert v-if="error" color="error" icon="mdi-alert-octagon-outline" outlined text class="my-4">
                    {{ error }}
                </v-alert>
                <div class="d-flex align-center mt-4">
                    <v-spacer />
                    <v-btn v-if="action === 'delete'" color="error" :loading="saving" @click="del">Delete</v-btn>
                    <template v-else-if="!readonly">
                        <v-btn color="secondary" :disabled="!valid" :loading="testing" class="mr-3" @click="test">Run test</v-btn>
                        <v-btn color="primary" :disabled="!valid" :loading="saving" @click="save">Save</v-btn>
                    </template>
                </div>
            </v-form>
        </v-card>
    </v-dialog>
</template>

<script>
const targetPlaceholders = {
    http: 'https://example.com/health',
    tcp: 'db.internal:5432',
    tls: 'example.com:443',
    dns: 'example.com',
};

export function latency(sec) {
    if (sec === null || sec === undefined) {
        return '';
    }
    if (sec < 0.001) {
        return '<1ms';
    }
    if (sec < 1) {
        return (sec * 1000).toFixed(sec < 0.01 ? 1 : 0) + 'ms';
    }
    return sec.toFixed(2) + 's';
}

function durationStr(ms) {
    if (!ms) {
        return '';
    }
    if (typeof ms === 'string') {
        return ms;
    }
    const s = Math.round(ms / 1000);
    if (s % 3600 === 0) {
        return s / 3600 + 'h';
    }
    if (s % 60 === 0) {
        return s / 60 + 'm';
    }
    return s + 's';
}

export default {
    props: {
        value: String,
        probe: Object,
        applications: Array,
        readonly: Boolean,
    },

    data() {
        const p = this.probe || {};
        const spec = { ...(p.spec || {}) };
        return {
            dialog: !!this.value,
            action: this.value,
            valid: false,
            saving: false,
            testing: false,
            error: '',
            result: null,
            form: {
                name: p.name || '',
                spec: {
                    type: spec.type || 'http',
                    target: spec.target || '',
                    interval: durationStr(spec.interval) || '60s',
                    timeout: durationStr(spec.timeout) || '10s',
                    method: spec.method || 'GET',
                    expected_status: spec.expected_status || '200-399',
                    body_contains: spec.body_contains || '',
                    headers: (spec.headers || []).map((h) => ({ ...h })),
                    follow_redirects: !!spec.follow_redirects,
                    tls_skip_verify: !!spec.tls_skip_verify,
                    dns_record_type: spec.dns_record_type || 'A',
                    dns_server: spec.dns_server || '',
                    application_id: spec.application_id || '',
                    paused: !!spec.paused,
                },
            },
            types: [
                { value: 'http', text: 'HTTP(S)' },
                { value: 'tcp', text: 'TCP' },
                { value: 'tls', text: 'TLS' },
                { value: 'dns', text: 'DNS' },
            ],
            methods: ['GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS'],
            dnsTypes: ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS'],
        };
    },

    watch: {
        dialog(v) {
            this.$emit('input', v ? this.value : '');
        },
    },

    computed: {
        title() {
            switch (this.action) {
                case 'new':
                    return 'Add a probe';
                case 'delete':
                    return 'Delete the probe';
            }
            return this.readonly ? 'Probe' : 'Edit the probe';
        },
        targetPlaceholder() {
            return targetPlaceholders[this.form.spec.type];
        },
        appItems() {
            return (this.applications || []).map((a) => ({ value: a.id, text: a.ns && a.ns !== '_' ? `${a.ns}/${a.name}` : a.name }));
        },
        phases() {
            const ph = (this.result && this.result.phases) || {};
            return ['dns', 'connect', 'tls', 'ttfb', 'total']
                .filter((k) => ph[k] !== undefined)
                .map((k) => `${k} ${latency(ph[k])}`)
                .join(' · ');
        },
        certExpiry() {
            const c = this.result && this.result.cert;
            if (!c) {
                return '';
            }
            const days = (new Date(c.not_after) - Date.now()) / 86400000;
            return `${new Date(c.not_after).toISOString().slice(0, 10)} (${Math.floor(days)} days)`;
        },
    },

    methods: {
        isName(v) {
            return /^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$/.test(v || '') || 'letters, digits, ".", "_" and "-"';
        },
        payload() {
            const spec = { ...this.form.spec, headers: this.form.spec.headers.filter((h) => h.key) };
            if (spec.application_id === null) {
                spec.application_id = '';
            }
            return { name: this.form.name, spec };
        },
        done(error, close) {
            if (error) {
                this.error = error;
                return;
            }
            this.$events.emit('refresh');
            if (close) {
                this.dialog = false;
            }
        },
        save() {
            this.saving = true;
            this.error = '';
            const cb = (data, error) => {
                this.saving = false;
                this.done(error, true);
            };
            if (this.action === 'new') {
                this.$api.createProbe(this.payload(), cb);
            } else {
                this.$api.updateProbe(this.probe.id, this.payload(), cb);
            }
        },
        del() {
            this.saving = true;
            this.error = '';
            this.$api.deleteProbe(this.probe.id, (data, error) => {
                this.saving = false;
                this.done(error, true);
            });
        },
        test() {
            this.testing = true;
            this.error = '';
            this.result = null;
            this.$api.testProbe(this.payload(), (data, error) => {
                this.testing = false;
                if (error) {
                    this.error = error;
                    return;
                }
                this.result = data;
            });
        },
    },
};
</script>

<style scoped>
.grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    column-gap: 16px;
}
.result {
    border: 1px solid var(--border);
    border-radius: var(--r-md);
    background: var(--surface-sunk);
    padding: 10px 12px;
}
@media (max-width: 600px) {
    .grid {
        grid-template-columns: 1fr;
    }
}
</style>
