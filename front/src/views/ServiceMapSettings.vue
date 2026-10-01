<template>
    <div class="sm-settings">
        <v-alert v-if="error" color="red" icon="mdi-alert-octagon-outline" outlined text dense>{{ error }}</v-alert>

        <h3 class="sm-title">Categories on the map</h3>
        <p class="sm-hint">
            How each category is drawn on the service map. <b>Collapsed</b> shows the whole category as one node that expands on click,
            <b>muted</b> also de-emphasizes it (e.g. a <var>legacy</var> category), <b>hidden</b> categories can still be toggled with the category
            checkboxes above the map.
        </p>
        <div class="sm-table">
            <div v-for="c in categories" :key="c" class="sm-row">
                <div class="sm-name">{{ c }}</div>
                <v-btn-toggle :value="mode(c)" @change="setMode(c, $event)" mandatory dense class="sm-modes" :aria-label="`map mode for ${c}`">
                    <v-btn v-for="m in modes" :key="m" :value="m" small :disabled="!editable" text>{{ m }}</v-btn>
                </v-btn-toggle>
            </div>
        </div>

        <h3 class="sm-title mt-6">Groups</h3>
        <p class="sm-hint">
            Applications are framed by their compose project (shards node agent), Kubernetes namespace, or the container-name prefix shared by two or
            more containers (<var>minust_parser</var>, <var>minust_postgres</var> &rarr; <var>minust</var>). Rules below take precedence; patterns are
            globs on the application name.
        </p>
        <div v-for="(g, i) in form.groups" :key="i" class="sm-group">
            <v-text-field v-model="g.name" label="group" outlined dense hide-details class="sm-group-name" :disabled="!editable" />
            <v-text-field
                v-model="g.patternsText"
                label="patterns, space separated"
                placeholder="minust_* minust-*"
                outlined
                dense
                hide-details
                class="flex-grow-1"
                :disabled="!editable"
            />
            <v-btn icon small :disabled="!editable" @click="form.groups.splice(i, 1)" aria-label="remove group"
                ><v-icon small>mdi-trash-can-outline</v-icon></v-btn
            >
        </div>
        <div class="d-flex align-center flex-wrap" style="gap: 12px">
            <v-btn small text color="primary" :disabled="!editable" @click="form.groups.push({ name: '', patternsText: '' })">
                <v-icon small left>mdi-plus</v-icon>Add a group rule
            </v-btn>
            <v-checkbox v-model="form.prefixGrouping" label="group containers by name prefix" dense hide-details class="mt-0" :disabled="!editable" />
        </div>

        <h3 class="sm-title mt-6">Node display names</h3>
        <p class="sm-hint">
            Rename nodes everywhere (node list, node page, map, alerts, MCP) without touching the agents. The shards node agent's
            <var>--hostname-override</var> does the same on the agent side.
        </p>
        <div class="sm-table">
            <div v-for="n in nodeRows" :key="n.key" class="sm-row">
                <div class="sm-name">
                    {{ n.hostname || n.key }}
                    <div class="sm-sub">{{ n.machine_id ? 'machine_id ' + n.machine_id.slice(0, 12) : 'hostname' }}</div>
                </div>
                <v-text-field
                    v-model="form.names[n.key]"
                    :placeholder="n.hostname"
                    outlined
                    dense
                    hide-details
                    class="sm-node-name"
                    :disabled="!editable"
                    :aria-label="`display name for ${n.hostname || n.key}`"
                />
            </div>
            <div v-if="!nodeRows.length" class="sm-hint">No nodes reported yet.</div>
        </div>

        <div class="d-flex align-center mt-4" style="gap: 12px">
            <v-btn color="primary" small :loading="saving" :disabled="!editable" @click="save">Save</v-btn>
            <span v-if="message" class="sm-hint">{{ message }}</span>
        </div>
    </div>
</template>

<script>
const modes = ['expanded', 'collapsed', 'muted', 'hidden'];

export default {
    data() {
        return {
            loading: false,
            saving: false,
            error: '',
            message: '',
            data: null,
            form: { modes: {}, groups: [], prefixGrouping: true, names: {} },
        };
    },

    mounted() {
        this.get();
    },

    computed: {
        modes() {
            return modes;
        },
        editable() {
            return !!(this.data && this.data.editable);
        },
        categories() {
            return (this.data && this.data.categories) || [];
        },
        nodeRows() {
            const rows = ((this.data && this.data.nodes) || []).map((n) => ({ key: n.machine_id || n.hostname, ...n }));
            const known = new Set(rows.map((r) => r.key));
            Object.keys(this.form.names).forEach((k) => {
                if (!known.has(k) && !rows.some((r) => r.hostname === k)) {
                    rows.push({ key: k, hostname: k, machine_id: '' });
                }
            });
            return rows;
        },
    },

    methods: {
        mode(c) {
            return this.form.modes[c] || (this.data && this.data.effective_category_modes[c]) || 'expanded';
        },
        setMode(c, m) {
            this.$set(this.form.modes, c, m);
        },
        get() {
            this.loading = true;
            this.$api.serviceMapSettings(null, (data, error) => {
                this.loading = false;
                if (error) {
                    this.error = error;
                    return;
                }
                this.set(data);
            });
        },
        set(data) {
            this.data = data;
            const s = data.settings || {};
            const names = {};
            Object.entries(s.node_display_names || {}).forEach(([k, v]) => (names[k] = v));
            (data.nodes || []).forEach((n) => {
                const key = n.machine_id || n.hostname;
                if (!(key in names)) {
                    names[key] = names[n.hostname] || '';
                    delete names[n.hostname];
                }
            });
            this.form = {
                modes: { ...(s.category_modes || {}) },
                groups: (s.groups || []).map((g) => ({ name: g.name, patternsText: (g.patterns || []).join(' ') })),
                prefixGrouping: !s.disable_prefix_grouping,
                names,
            };
        },
        save() {
            this.error = '';
            this.message = '';
            const names = {};
            Object.entries(this.form.names).forEach(([k, v]) => {
                if (v && v.trim()) {
                    names[k] = v.trim();
                }
            });
            const form = {
                category_modes: this.form.modes,
                groups: this.form.groups
                    .map((g) => ({ name: g.name.trim(), patterns: g.patternsText.split(/\s+/).filter(Boolean) }))
                    .filter((g) => g.name || g.patterns.length),
                disable_prefix_grouping: !this.form.prefixGrouping,
                node_display_names: names,
            };
            this.saving = true;
            this.$api.serviceMapSettings(form, (data, error) => {
                this.saving = false;
                if (error) {
                    this.error = error;
                    return;
                }
                this.set(data);
                this.message = 'Saved. Node names apply on the next data refresh.';
                this.$events.emit('refresh');
            });
        },
    },
};
</script>

<style scoped>
.sm-settings {
    max-width: 860px;
}
.sm-title {
    font-size: 15px;
    font-weight: 600;
    color: var(--text-1);
    margin-bottom: 4px;
}
.sm-hint {
    font-size: 13px;
    color: var(--text-2);
}
.sm-table {
    border: 1px solid var(--border);
    border-radius: var(--r-md);
    background: var(--surface);
    overflow: hidden;
}
.sm-row {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 8px 12px;
    border-bottom: 1px solid var(--border-soft);
}
.sm-row:last-child {
    border-bottom: none;
}
.sm-name {
    flex: 0 0 200px;
    font-size: 13px;
    color: var(--text-1);
    overflow: hidden;
    text-overflow: ellipsis;
}
.sm-sub {
    font-size: 11px;
    color: var(--text-3);
    font-family: var(--mono);
}
.sm-modes {
    flex-wrap: wrap;
}
.sm-group {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 8px;
}
.sm-group-name {
    flex: 0 0 180px;
}
.sm-node-name {
    max-width: 280px;
}
@media (max-width: 600px) {
    .sm-row,
    .sm-group {
        flex-wrap: wrap;
    }
    .sm-name,
    .sm-group-name {
        flex-basis: 100%;
    }
}
</style>
