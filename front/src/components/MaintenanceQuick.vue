<template>
    <v-menu offset-y left :close-on-content-click="false" v-model="open">
        <template #activator="{ on, attrs }">
            <v-btn small text v-bind="attrs" v-on="on" class="quick" title="Mute this application's notifications, e.g. during a deployment">
                <v-icon small left>mdi-wrench-clock</v-icon>
                <template v-if="done">In maintenance until {{ $format.date(done.ends_at, '{HH}:{mm}') }}</template>
                <template v-else>Maintenance</template>
            </v-btn>
        </template>
        <v-card class="pa-3" min-width="280">
            <div class="font-weight-medium mb-1">Mute notifications for {{ name }}</div>
            <div class="caption mb-2">
                Alerts and incidents of this application are still created, but nothing is sent while the window is active.
            </div>
            <div class="d-flex flex-wrap" style="gap: 6px">
                <v-btn v-for="m in [15, 30, 60, 120, 240]" :key="m" small outlined :loading="busy === m" :disabled="!!busy" @click="create(m)">
                    {{ m < 60 ? m + 'm' : m / 60 + 'h' }}
                </v-btn>
            </div>
            <v-text-field v-model="comment" dense outlined hide-details placeholder="Reason (optional)" class="mt-3" />
            <div v-if="error" class="error--text caption mt-2">{{ error }}</div>
            <div v-if="notice" class="caption mt-2">{{ notice }}</div>
            <router-link
                :to="{ name: 'overview', params: { view: 'alerts', id: 'maintenance' }, query: $utils.contextQuery() }"
                class="caption d-block mt-2"
            >
                All maintenance windows
            </router-link>
        </v-card>
    </v-menu>
</template>

<script>
export default {
    props: {
        appId: { type: String, required: true },
    },

    data() {
        return { open: false, busy: 0, error: '', notice: '', comment: '', done: null };
    },

    computed: {
        name() {
            return this.$utils.appId(this.appId).name;
        },
        pattern() {
            // maintenance scopes match application ids without the cluster id: namespace:Kind:name
            return this.appId.split(':').slice(1).join(':');
        },
    },

    methods: {
        create(minutes) {
            this.busy = minutes;
            this.error = '';
            this.notice = '';
            const form = {
                name: `${this.name} maintenance`,
                duration_minutes: minutes,
                comment: this.comment.trim(),
                scope: { application_patterns: [this.pattern] },
            };
            this.$api.saveMaintenanceWindow(null, form, (data, error, status) => {
                this.busy = 0;
                if (error) {
                    this.error = error;
                    return;
                }
                if (status === 202) {
                    this.notice = `Waiting for approval (#${data.approval_id}).`;
                    return;
                }
                this.done = data;
                this.open = false;
            });
        },
    },
};
</script>

<style scoped>
.quick {
    color: var(--text-2) !important;
}
</style>
