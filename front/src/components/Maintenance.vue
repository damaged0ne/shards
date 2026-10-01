<template>
    <div>
        <div class="d-flex flex-wrap align-center mb-3" style="gap: 12px">
            <div class="caption" style="max-width: 760px">
                Maintenance windows mute notifications for planned work (deployments, restarts, migrations). Alerts and incidents are still created
                and shown as <span class="status-chip info">maintenance</span>, but nothing is sent to Slack, PagerDuty, webhooks, etc.
            </div>
            <v-spacer />
            <v-checkbox v-model="showEnded" label="Show ended" dense hide-details class="mt-0" @change="get" />
            <v-btn color="primary" small depressed :disabled="!canEdit" @click="edit(null)"><v-icon small left>mdi-plus</v-icon>New window</v-btn>
        </div>

        <v-alert v-if="notice" color="info" outlined text dense dismissible @input="notice = ''">{{ notice }}</v-alert>

        <div class="list">
            <div v-if="!loading && !windows.length" class="empty">No maintenance windows{{ showEnded ? '' : ' scheduled' }}.</div>
            <div v-for="w in windows" :key="w.id" class="row" :class="{ dim: w.status === 'ended' || w.status === 'expired' }">
                <span class="status-chip" :class="statusChip(w.status)">{{ w.status }}</span>
                <div class="grow">
                    <div class="name">
                        {{ w.name }}
                        <v-icon v-if="w.recurrence" size="14" class="ml-1" title="Recurring">mdi-repeat</v-icon>
                    </div>
                    <div class="secondary-text">
                        <span>{{ schedule(w) }}</span>
                        <span v-if="w.status === 'active' && w.current_to"> · ends {{ $format.date(w.current_to, '{MMM} {DD}, {HH}:{mm}') }}</span>
                        <span v-else-if="w.status === 'scheduled' && w.current_from && w.recurrence">
                            · next {{ $format.date(w.current_from, '{MMM} {DD}, {HH}:{mm}') }}</span
                        >
                    </div>
                    <div class="secondary-text"><v-icon size="13">mdi-target</v-icon> {{ scope(w.scope) }}</div>
                    <div v-if="w.comment" class="comment">{{ w.comment }}</div>
                </div>
                <div class="by">
                    <v-icon v-if="w.created_by_kind === 'agent'" size="14" title="Created by an agent">mdi-robot-outline</v-icon>
                    {{ w.created_by }}
                    <div v-if="w.ended_by" class="secondary-text">ended by {{ w.ended_by }}</div>
                </div>
                <div class="actions">
                    <v-btn
                        v-if="w.status === 'active' || w.status === 'scheduled'"
                        small
                        outlined
                        :disabled="!canEdit"
                        :loading="busy === w.id"
                        @click="end(w)"
                    >
                        End now
                    </v-btn>
                    <v-menu offset-y left>
                        <template #activator="{ on, attrs }">
                            <v-btn icon small v-bind="attrs" v-on="on" :disabled="!canEdit"><v-icon small>mdi-dots-vertical</v-icon></v-btn>
                        </template>
                        <v-list dense>
                            <v-list-item @click="edit(w)"><v-icon small class="mr-2">mdi-pencil</v-icon>Edit</v-list-item>
                            <v-list-item @click="duplicate(w)"><v-icon small class="mr-2">mdi-content-copy</v-icon>Duplicate</v-list-item>
                            <v-list-item @click="remove(w)"><v-icon small class="mr-2">mdi-delete</v-icon>Delete</v-list-item>
                        </v-list>
                    </v-menu>
                </div>
            </div>
        </div>

        <MaintenanceForm v-model="form.active" :window="form.window" :rules="rules" @saved="saved" />
    </div>
</template>

<script>
import MaintenanceForm from '@/components/MaintenanceForm.vue';
import { maintenanceScheduleText, maintenanceScopeText, maintenanceStatusChip } from '@/utils/workflow';

export default {
    components: { MaintenanceForm },

    data() {
        return {
            windows: [],
            rules: [],
            loading: false,
            showEnded: false,
            busy: null,
            notice: '',
            canEdit: true,
            form: { active: false, window: null },
        };
    },

    mounted() {
        this.get();
        this.$events.watch(this, this.get, 'refresh');
        this.$api.getAlertingRules((data) => {
            if (data && data.rules) {
                this.rules = data.rules.map((r) => ({ id: r.id, name: r.name })).sort((a, b) => a.name.localeCompare(b.name));
            }
        });
    },

    methods: {
        get() {
            this.loading = true;
            this.$emit('loading', true);
            this.$api.getMaintenanceWindows(this.showEnded, (data, error, status) => {
                this.loading = false;
                this.$emit('loading', false);
                if (error) {
                    if (status === 403) {
                        this.canEdit = false;
                    }
                    this.$emit('error', error);
                    return;
                }
                this.$emit('error', '');
                this.windows = data || [];
            });
        },
        statusChip: maintenanceStatusChip,
        scope: maintenanceScopeText,
        schedule(w) {
            return maintenanceScheduleText(w, this.$format);
        },
        edit(w) {
            this.form = { active: true, window: w };
        },
        duplicate(w) {
            this.form = { active: true, window: { ...w, id: undefined, name: w.name + ' (copy)', starts_at: undefined, ends_at: undefined } };
        },
        saved(data, status) {
            this.notice = status === 202 ? `The request is waiting for approval (#${data.approval_id}).` : '';
            this.get();
        },
        end(w) {
            this.busy = w.id;
            this.$api.endMaintenanceWindow(w.id, '', (data, error) => {
                this.busy = null;
                if (error) {
                    this.$emit('error', error);
                    return;
                }
                this.get();
            });
        },
        remove(w) {
            if (!confirm(`Delete the maintenance window "${w.name}"?`)) {
                return;
            }
            this.$api.deleteMaintenanceWindow(w.id, (data, error) => {
                if (error) {
                    this.$emit('error', error);
                    return;
                }
                this.get();
            });
        },
    },
};
</script>

<style scoped>
.list {
    border: 1px solid var(--border);
    border-radius: var(--r-lg);
    background: var(--surface);
}
.empty {
    padding: 14px;
    font-size: 13px;
    color: var(--text-3);
}
.row {
    display: flex;
    align-items: flex-start;
    gap: 12px;
    padding: 10px 14px;
}
.row + .row {
    border-top: 1px solid var(--border-soft);
}
.row .status-chip {
    margin-top: 2px;
    min-width: 78px;
    justify-content: center;
}
.dim {
    opacity: 0.7;
}
.grow {
    flex: 1;
    min-width: 0;
}
.name {
    font-weight: 600;
    font-size: 14px;
}
.secondary-text {
    font-size: 12.5px;
    color: var(--text-2);
}
.secondary-text .v-icon {
    color: var(--text-3) !important;
}
.comment {
    font-size: 13px;
    margin-top: 4px;
}
.by {
    flex: none;
    font-size: 12.5px;
    width: 160px;
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
}
.actions {
    flex: none;
    display: flex;
    align-items: center;
    gap: 4px;
}
@media (max-width: 700px) {
    .row {
        flex-wrap: wrap;
    }
    .by {
        width: auto;
    }
}
</style>
