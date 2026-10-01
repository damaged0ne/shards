<template>
    <v-dialog :value="value" @input="$emit('input', $event)" max-width="760" scrollable>
        <v-card>
            <v-card-title class="d-flex align-center">
                {{ agent ? `Edit ${agent.name}` : 'New agent' }}
                <v-spacer />
                <v-btn icon small @click="$emit('input', false)"><v-icon small>mdi-close</v-icon></v-btn>
            </v-card-title>
            <v-card-text class="pt-2">
                <v-form v-model="valid" ref="form">
                    <div class="grid2">
                        <v-text-field
                            v-model="f.name"
                            label="Name"
                            outlined
                            dense
                            :rules="[$validators.notEmpty, nameRule]"
                            hint="Used for @mentions, e.g. @triage-bot"
                            persistent-hint
                        />
                        <v-select
                            v-if="users.length"
                            v-model="f.owner_id"
                            :items="users"
                            item-text="name"
                            item-value="id"
                            label="Owner"
                            outlined
                            dense
                            hint="The agent acts on behalf of this user; its scope never exceeds the owner's role"
                            persistent-hint
                        />
                    </div>
                    <v-text-field v-model="f.description" label="Description" outlined dense class="mt-3" hide-details />
                    <div class="grid2 mt-3">
                        <v-text-field v-model="f.vendor" label="Kind / vendor" placeholder="Claude Code, custom bot, …" outlined dense hide-details />
                        <v-text-field v-model="f.model" label="Model" placeholder="e.g. claude-opus" outlined dense hide-details />
                    </div>

                    <div class="section">Scope</div>
                    <div class="scopes">
                        <label v-for="s in scopes" :key="s.value" class="scope" :class="{ selected: f.scope === s.value }">
                            <input type="radio" :value="s.value" v-model="f.scope" />
                            <Chip :tone="scopeClass[s.value]">{{ s.name }}</Chip>
                            <span class="caption">{{ s.description }}</span>
                        </label>
                    </div>

                    <div class="grid2 mt-3">
                        <v-text-field v-model="expires" type="date" label="Expires (optional)" outlined dense clearable hide-details />
                        <v-select
                            v-model="f.allowed_projects"
                            :items="projects"
                            item-text="name"
                            item-value="id"
                            label="Allowed projects"
                            placeholder="All projects the owner can access"
                            persistent-placeholder
                            multiple
                            outlined
                            dense
                            hide-details
                        />
                    </div>
                    <v-checkbox v-if="agent" v-model="f.disabled" label="Disabled (keys stop working)" dense hide-details class="mt-2" />

                    <div class="section d-flex align-center">
                        Dispatch webhook
                        <v-switch v-model="d.enabled" inset dense hide-details class="mt-0 ml-3" />
                    </div>
                    <div class="caption grey--text mb-2">
                        Wake the agent up with a signed POST (X-Shards-Signature: sha256=HMAC of "&lt;timestamp&gt;.&lt;body&gt;") instead of polling.
                    </div>
                    <template v-if="d.enabled">
                        <v-text-field
                            v-model="d.url"
                            label="Webhook URL"
                            placeholder="https://agent.example.com/shards"
                            outlined
                            dense
                            :rules="[$validators.notEmpty]"
                        />
                        <div class="d-flex align-center" style="gap: 8px">
                            <v-text-field
                                v-model="d.secret"
                                label="Signing secret"
                                :placeholder="agent && agent.secret_set ? 'unchanged (set)' : 'none'"
                                persistent-placeholder
                                outlined
                                dense
                                hide-details
                                class="mono"
                            />
                            <v-btn small outlined @click="d.secret = randomSecret()">Generate</v-btn>
                        </div>
                        <div class="caption grey--text mt-1 mb-3">Shown only now — copy it into the agent's webhook receiver.</div>
                        <div class="caption mb-1">Events</div>
                        <div class="events">
                            <v-checkbox
                                v-for="e in events"
                                :key="e.value"
                                v-model="d.events"
                                :value="e.value"
                                :label="e.name"
                                dense
                                hide-details
                                class="mt-0"
                            />
                        </div>
                        <div class="caption grey--text mb-3">Manual "Ask agent" tasks are always delivered.</div>
                        <div class="grid2">
                            <v-select v-model="d.min_severity" :items="severities" label="Min severity" outlined dense hide-details />
                            <v-combobox
                                v-model="d.categories"
                                label="App categories"
                                placeholder="all"
                                persistent-placeholder
                                multiple
                                small-chips
                                outlined
                                dense
                                hide-details
                            />
                        </div>
                        <v-combobox
                            v-model="d.app_patterns"
                            label="Application patterns"
                            placeholder="namespace:Kind:name globs, e.g. prod:Deployment:*"
                            persistent-placeholder
                            multiple
                            small-chips
                            outlined
                            dense
                            hide-details
                            class="mt-3"
                        />
                        <div class="grid2 mt-3">
                            <v-text-field
                                v-model.number="d.rate_limit_per_hour"
                                type="number"
                                label="Rate limit / hour"
                                outlined
                                dense
                                hide-details
                            />
                            <v-text-field
                                v-model.number="d.dedup_minutes"
                                type="number"
                                label="Dedup window (min) per incident/alert"
                                outlined
                                dense
                                hide-details
                            />
                        </div>
                    </template>
                </v-form>
                <v-alert v-if="error" color="error" outlined text dense class="mt-3 mb-0">{{ error }}</v-alert>
            </v-card-text>
            <v-card-actions>
                <v-spacer />
                <v-btn text @click="$emit('input', false)">Cancel</v-btn>
                <v-btn color="primary" :disabled="!valid" :loading="saving" @click="save">{{ agent ? 'Save' : 'Create' }}</v-btn>
            </v-card-actions>
        </v-card>
    </v-dialog>
</template>

<script>
import { scopes, scopeClass, events, randomSecret } from './agents';
import Chip from './Chip.vue';

function emptyDispatch() {
    return {
        enabled: false,
        url: '',
        secret: '',
        events: ['incident_opened', 'alert_fired', 'mention'],
        min_severity: 'warning',
        categories: [],
        app_patterns: [],
        rate_limit_per_hour: 30,
        dedup_minutes: 30,
    };
}

export default {
    components: { Chip },
    inject: { shell: { default: null } },

    props: {
        value: Boolean,
        agent: Object,
        users: { type: Array, default: () => [] },
        defaultOwnerId: Number,
    },

    data() {
        return {
            valid: false,
            saving: false,
            error: '',
            f: {},
            d: emptyDispatch(),
            expires: '',
        };
    },

    computed: {
        scopes() {
            return scopes;
        },
        scopeClass() {
            return scopeClass;
        },
        events() {
            return events;
        },
        severities() {
            return [
                { text: 'warning and above', value: 'warning' },
                { text: 'critical only', value: 'critical' },
            ];
        },
        projects() {
            return (this.shell && this.shell.projects) || [];
        },
    },

    watch: {
        value: {
            handler(v) {
                if (v) {
                    this.reset();
                }
            },
            immediate: true,
        },
    },

    methods: {
        randomSecret,
        nameRule(v) {
            return /^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$/.test(v || '') || "letters, digits, '-', '_', '.'";
        },
        reset() {
            const a = this.agent;
            this.error = '';
            this.f = {
                name: a ? a.name : '',
                description: a ? a.description : '',
                vendor: a ? a.vendor : '',
                model: a ? a.model : '',
                owner_id: a ? a.owner_id : this.defaultOwnerId,
                scope: a ? a.scope : 'triage',
                allowed_projects: a ? a.allowed_projects || [] : [],
                disabled: a ? a.disabled : false,
            };
            this.d = { ...emptyDispatch(), ...((a && a.dispatch) || {}), secret: '' };
            this.d.categories = this.d.categories || [];
            this.d.app_patterns = this.d.app_patterns || [];
            this.d.events = this.d.events || [];
            this.expires = a && a.expires_at ? new Date(a.expires_at).toISOString().slice(0, 10) : '';
        },
        save() {
            const form = { ...this.f, dispatch: { ...this.d }, expires_at: this.expires ? new Date(this.expires + 'T23:59:59').getTime() : 0 };
            this.saving = true;
            const cb = (data, error) => {
                this.saving = false;
                if (error) {
                    this.error = error;
                    return;
                }
                this.$emit('input', false);
                this.$emit('saved', data);
            };
            if (this.agent) {
                this.$api.updateAgent(this.agent.id, form, cb);
            } else {
                this.$api.createAgent(form, cb);
            }
        },
    },
};
</script>

<style scoped>
.grid2 {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
    gap: 12px;
}
.section {
    margin: 20px 0 8px;
    font-weight: 600;
    font-size: 13px;
    color: var(--text-1);
}
.scopes {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
    gap: 8px;
}
.scope {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 10px;
    border: 1px solid var(--border);
    border-radius: var(--r-md);
    cursor: pointer;
    color: var(--text-2);
}
.scope input {
    display: none;
}
.scope .agent-chip {
    align-self: flex-start;
}
.scope.selected {
    border-color: var(--accent-8);
    background: var(--accent-2);
}
.events {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
    gap: 0 12px;
}
</style>
