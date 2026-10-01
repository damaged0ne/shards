<template>
    <div>
        <TopBar v-if="!noTitle" :title="title">
            <template v-if="$slots.subtitle" #crumbs>
                <div class="d-flex align-center flex-nowrap gap-2" style="min-width: 0">
                    <template v-if="$vuetify.breakpoint.smAndUp">
                        <router-link :to="{ name: 'overview', params: { view }, query: $utils.contextQuery() }">{{ title }}</router-link>
                        <span class="crumb-sep">/</span>
                    </template>
                    <span class="nowrap"><slot name="subtitle"></slot></span>
                </div>
            </template>
            <template #actions>
                <TimePicker :small="$vuetify.breakpoint.xsOnly" />
            </template>
        </TopBar>

        <v-progress-linear v-if="loading" indeterminate height="2" color="primary" class="views-progress" />

        <v-alert v-if="error" color="error" icon="mdi-alert-octagon-outline" outlined text>
            {{ error }}
        </v-alert>

        <slot v-else></slot>
    </div>
</template>

<script>
import TimePicker from '@/components/TimePicker.vue';
import TopBar from '@/components/TopBar.vue';

// group and tile define how the view is presented in the navigation rail.
export const views = {
    home: { name: 'Home', icon: 'mdi-inbox-outline', group: 'home', tile: 'accent' },
    applications: { name: 'Applications', icon: 'mdi-apps', group: 'health', tile: 'accent' },
    incidents: { name: 'Incidents', icon: 'mdi-alert-outline', group: 'health', tile: 'danger' },
    alerts: { name: 'Alerts', icon: 'mdi-bell-outline', group: 'health', tile: 'warning' },
    map: { name: 'Service Map', icon: 'mdi-map-outline', group: 'explore', tile: 'cyan' },
    traces: { name: 'Traces', icon: 'mdi-chart-timeline', group: 'explore', tile: 'info' },
    logs: { name: 'Logs', icon: 'mdi-text-search', group: 'explore', tile: 'slate' },
    nodes: { name: 'Nodes', icon: 'mdi-server', group: 'infrastructure', tile: 'slate' },
    kubernetes: { name: 'Kubernetes', icon: 'mdi-ship-wheel', group: 'infrastructure', tile: 'info' },
    costs: { name: 'Costs', icon: 'mdi-currency-usd', group: 'infrastructure', tile: 'success' },
    risks: { name: 'Risks', icon: 'mdi-weather-lightning', group: 'infrastructure', tile: 'pink' },
    dashboards: { name: 'Dashboards', icon: 'mdi-view-dashboard-outline', group: 'explore', tile: 'purple' },
};

export default {
    props: {
        loading: Boolean,
        error: String,
        noTitle: Boolean,
    },

    components: { TimePicker, TopBar },

    computed: {
        view() {
            return this.$route.params.view;
        },
        title() {
            const v = views[this.view];
            if (!v) {
                return null;
            }
            return v.name;
        },
    },
};
</script>

<style scoped>
.crumb-sep {
    color: var(--text-3);
    font-weight: 400;
}
.views-progress {
    position: fixed !important;
    top: 0;
    left: 0;
    right: 0;
    z-index: 10;
}
</style>
