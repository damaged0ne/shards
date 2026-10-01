<template>
    <v-dialog :value="value" @input="$emit('input', $event)" max-width="640" scrollable>
        <v-card>
            <v-card-title class="d-flex">
                {{ window && window.id ? 'Edit maintenance window' : 'New maintenance window' }}
                <v-spacer />
                <v-btn icon small @click="$emit('input', false)"><v-icon small>mdi-close</v-icon></v-btn>
            </v-card-title>
            <v-card-text class="pt-2">
                <div class="caption mb-3">
                    While a window is active, matching alerts and incidents are still created (and marked "in maintenance") but no notifications are
                    sent. When it ends, the ones still firing are notified; the ones resolved meanwhile are not.
                </div>
                <v-form ref="form" v-model="valid" @submit.prevent="save">
                    <v-text-field
                        v-model="form.name"
                        label="Name"
                        outlined
                        dense
                        :rules="[$validators.notEmpty]"
                        placeholder="e.g. payments deploy v2.3"
                    />

                    <v-btn-toggle v-model="mode" mandatory dense class="mb-3">
                        <v-btn value="once" small>One-off</v-btn>
                        <v-btn value="weekly" small>Weekly</v-btn>
                    </v-btn-toggle>

                    <template v-if="mode === 'once'">
                        <div v-if="!form.id" class="d-flex flex-wrap align-center mb-3" style="gap: 6px">
                            <span class="caption mr-1">From now for</span>
                            <v-chip
                                v-for="d in quick"
                                :key="d.minutes"
                                small
                                label
                                :color="duration === d.minutes ? 'primary' : undefined"
                                :outlined="duration !== d.minutes"
                                @click="setQuick(d.minutes)"
                            >
                                {{ d.name }}
                            </v-chip>
                            <v-chip small label :outlined="duration !== 0" :color="duration === 0 ? 'primary' : undefined" @click="duration = 0"
                                >custom</v-chip
                            >
                        </div>
                        <div v-if="duration === 0 || form.id" class="d-flex flex-wrap" style="gap: 12px">
                            <v-text-field
                                v-model="startsAt"
                                type="datetime-local"
                                label="Starts"
                                outlined
                                dense
                                hide-details
                                style="min-width: 220px"
                            />
                            <v-text-field v-model="endsAt" type="datetime-local" label="Ends" outlined dense hide-details style="min-width: 220px" />
                        </div>
                    </template>
                    <template v-else>
                        <div class="d-flex flex-wrap mb-1" style="gap: 2px 10px">
                            <v-checkbox
                                v-for="(d, i) in weekdays"
                                :key="d"
                                v-model="recurrence.weekdays"
                                :value="i"
                                :label="d"
                                dense
                                hide-details
                                class="mt-0"
                            />
                        </div>
                        <div class="d-flex flex-wrap mt-3" style="gap: 12px">
                            <v-text-field
                                v-model="recurrence.start_time"
                                type="time"
                                label="Starts at"
                                outlined
                                dense
                                hide-details
                                style="max-width: 140px"
                            />
                            <v-text-field
                                v-model.number="recurrence.duration_minutes"
                                type="number"
                                label="Duration, minutes"
                                outlined
                                dense
                                hide-details
                                style="max-width: 160px"
                            />
                            <v-text-field v-model="recurrence.timezone" label="Timezone" outlined dense hide-details style="max-width: 200px" />
                        </div>
                    </template>

                    <div class="subtitle-2 mt-5 mb-1">Scope</div>
                    <div class="caption mb-2">Empty = everything. Non-empty fields must all match.</div>
                    <v-combobox
                        v-model="form.scope.application_patterns"
                        label="Applications (namespace:Kind:name globs)"
                        multiple
                        small-chips
                        deletable-chips
                        outlined
                        dense
                        :items="appSuggestions"
                    />
                    <v-combobox v-model="form.scope.categories" label="Application categories" multiple small-chips deletable-chips outlined dense />
                    <v-combobox v-model="form.scope.node_patterns" label="Nodes (name globs)" multiple small-chips deletable-chips outlined dense />
                    <v-combobox
                        v-model="form.scope.alerting_rule_ids"
                        label="Alerting rules"
                        multiple
                        small-chips
                        deletable-chips
                        outlined
                        dense
                        :items="rules"
                        item-text="name"
                        item-value="id"
                        :return-object="false"
                    />
                    <v-textarea v-model="form.comment" label="Comment" outlined dense rows="2" auto-grow hide-details />
                </v-form>
                <v-alert v-if="error" color="error" outlined text dense class="mt-3 mb-0">{{ error }}</v-alert>
            </v-card-text>
            <v-card-actions class="px-6 pb-4">
                <v-spacer />
                <v-btn text @click="$emit('input', false)">Cancel</v-btn>
                <v-btn color="primary" depressed :loading="saving" :disabled="!valid" @click="save">{{ form.id ? 'Save' : 'Create' }}</v-btn>
            </v-card-actions>
        </v-card>
    </v-dialog>
</template>

<script>
import { weekdayNames } from '@/utils/workflow';

function toLocalInput(ms) {
    if (!ms) {
        return '';
    }
    const d = new Date(ms);
    const pad = (n) => String(n).padStart(2, '0');
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export default {
    props: {
        value: Boolean,
        window: Object,
        rules: { type: Array, default: () => [] },
        appSuggestions: { type: Array, default: () => [] },
    },

    data() {
        return this.initial();
    },

    watch: {
        value(v) {
            if (v) {
                Object.assign(this.$data, this.initial());
            }
        },
    },

    computed: {
        weekdays() {
            return weekdayNames();
        },
        quick() {
            return [
                { name: '15m', minutes: 15 },
                { name: '30m', minutes: 30 },
                { name: '1h', minutes: 60 },
                { name: '2h', minutes: 120 },
                { name: '4h', minutes: 240 },
                { name: '1d', minutes: 1440 },
            ];
        },
    },

    methods: {
        initial() {
            const w = this.window || {};
            const scope = w.scope || {};
            const now = Date.now();
            return {
                valid: true,
                saving: false,
                error: '',
                mode: w.recurrence ? 'weekly' : 'once',
                duration: w.id ? 0 : 60,
                startsAt: toLocalInput(w.starts_at || now),
                endsAt: toLocalInput(w.ends_at || now + 3600 * 1000),
                recurrence: {
                    weekdays: [],
                    start_time: '02:00',
                    duration_minutes: 60,
                    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC',
                    ...(w.recurrence || {}),
                },
                form: {
                    id: w.id,
                    name: w.name || '',
                    comment: w.comment || '',
                    scope: {
                        application_patterns: [...(scope.application_patterns || [])],
                        categories: [...(scope.categories || [])],
                        node_patterns: [...(scope.node_patterns || [])],
                        alerting_rule_ids: [...(scope.alerting_rule_ids || [])],
                    },
                },
            };
        },
        setQuick(minutes) {
            this.duration = minutes;
        },
        save() {
            if (!this.$refs.form.validate()) {
                return;
            }
            const body = { name: this.form.name, comment: this.form.comment, scope: this.form.scope };
            if (this.mode === 'weekly') {
                body.recurrence = { ...this.recurrence, duration_minutes: Number(this.recurrence.duration_minutes) };
                body.starts_at = this.window && this.window.starts_at ? this.window.starts_at : Date.now();
            } else if (this.duration && !this.form.id) {
                body.duration_minutes = this.duration;
            } else {
                body.starts_at = new Date(this.startsAt).getTime();
                body.ends_at = new Date(this.endsAt).getTime();
            }
            this.saving = true;
            this.error = '';
            this.$api.saveMaintenanceWindow(this.form.id, body, (data, error, status) => {
                this.saving = false;
                if (error) {
                    this.error = error;
                    return;
                }
                this.$emit('saved', data, status);
                this.$emit('input', false);
            });
        },
    },
};
</script>
