<template>
    <v-app>
        <v-navigation-drawer
            v-if="menu"
            v-model="drawer"
            app
            floating
            :permanent="!mobile"
            :temporary="mobile"
            :mini-variant="!mobile && collapsed"
            mini-variant-width="64"
            width="232"
            class="rail"
            :class="{ mini: !mobile && collapsed }"
        >
            <div class="rail-inner">
                <router-link class="brand" :to="project ? { name: 'overview', query: $utils.contextQuery() } : { name: 'index' }">
                    <img :src="`${$coroot.base_path}static/brand/icon.svg`" width="26" height="26" alt="" class="brand-icon" />
                    <span class="wordmark">shards</span>
                </router-link>

                <nav v-if="project" class="rail-nav" aria-label="Sections">
                    <div v-for="g in navGroups" :key="g.id" class="rail-group">
                        <div class="rail-title">{{ g.name }}</div>
                        <router-link
                            v-for="item in g.items"
                            :key="item.id"
                            :to="{ name: 'overview', params: { view: item.id, id: undefined, report: undefined }, query: getMenuQuery(item.id) }"
                            class="rail-item"
                            :class="{ active: item.id === view }"
                            :aria-current="item.id === view ? 'page' : undefined"
                            :title="collapsed && !mobile ? item.name : undefined"
                        >
                            <span class="tile" :class="`tile-${item.tile}`">
                                <v-icon size="14">{{ item.icon }}</v-icon>
                                <span v-if="item.dot" class="tile-dot" :class="item.dot" />
                            </span>
                            <span class="label">{{ item.name }}</span>
                            <span v-if="item.badges.length" class="badges">
                                <span v-for="b in item.badges" :key="b.class" class="badge" :class="b.class">{{ b.value }}</span>
                            </span>
                        </router-link>
                    </div>
                </nav>

                <div class="rail-spacer" />

                <div class="rail-foot">
                    <router-link
                        :to="{ name: project ? 'project_settings' : 'project_new' }"
                        class="rail-item"
                        :class="{ active: $route.name === 'project_settings' || $route.name === 'project_new' }"
                        :title="collapsed && !mobile ? 'Settings' : undefined"
                    >
                        <span class="tile tile-plain"><v-icon size="15">mdi-cog-outline</v-icon></span>
                        <span class="label">Settings</span>
                    </router-link>

                    <v-menu right offset-x nudge-right="8" top>
                        <template #activator="{ on, attrs }">
                            <button type="button" class="rail-item" v-bind="attrs" v-on="on" :title="collapsed && !mobile ? 'Help' : undefined">
                                <span class="tile tile-plain"><v-icon size="15">mdi-help-circle-outline</v-icon></span>
                                <span class="label">Help</span>
                            </button>
                        </template>
                        <v-list dense class="py-1">
                            <v-list-item :href="$utils.docsUrl('')" target="_blank">
                                <v-icon small class="mr-2">mdi-book-open-outline</v-icon>Documentation
                            </v-list-item>
                            <v-list-item :href="repoUrl" target="_blank"> <v-icon small class="mr-2">mdi-github</v-icon>Source code </v-list-item>
                            <v-divider class="my-1" />
                            <v-list-item :href="`${repoUrl}/releases`" target="_blank">
                                <span class="grey--text">Version:</span>&nbsp;<span class="mono">{{ $coroot.version }}</span>
                            </v-list-item>
                        </v-list>
                    </v-menu>

                    <button v-if="!mobile" type="button" class="rail-item" @click="toggleCollapsed" :title="collapsed ? 'Expand' : undefined">
                        <span class="tile tile-plain">
                            <v-icon size="15">{{ collapsed ? 'mdi-chevron-double-right' : 'mdi-chevron-double-left' }}</v-icon>
                        </span>
                        <span class="label">Collapse</span>
                    </button>

                    <!-- v-menu.eager keeps ThemeSelector mounted -->
                    <v-menu v-if="user" right offset-x nudge-right="8" top eager>
                        <template #activator="{ on, attrs }">
                            <button type="button" class="user" v-bind="attrs" v-on="on" :title="user.name">
                                <span class="avatar">{{ initials }}</span>
                                <span class="user-text">
                                    <span class="user-name">{{ user.name }}</span>
                                    <span v-if="user.role" class="user-role">{{ user.role }}</span>
                                </span>
                                <v-icon size="16" class="user-more">mdi-unfold-more-horizontal</v-icon>
                            </button>
                        </template>
                        <v-list dense class="py-1" min-width="220">
                            <div class="px-4 py-2">
                                <div class="font-weight-medium">{{ user.name }}</div>
                                <div v-if="user.email" class="caption">login: {{ user.email }}</div>
                                <div v-if="user.role" class="caption">role: {{ user.role }}</div>
                            </div>
                            <v-divider class="my-1" />
                            <v-subheader>Theme</v-subheader>
                            <ThemeSelector />
                            <template v-if="!user.anonymous">
                                <v-divider class="my-1" />
                                <v-list-item @click="changePassword = true">Change password</v-list-item>
                                <v-list-item @click="apiKeys = true">API keys</v-list-item>
                                <v-list-item :to="{ name: 'logout' }">Sign out</v-list-item>
                            </template>
                        </v-list>
                    </v-menu>
                </div>
            </div>
        </v-navigation-drawer>

        <TopBar v-if="menu && $route.name !== 'overview'" :title="pageTitle" />

        <v-main>
            <v-container fluid class="content">
                <v-alert
                    v-if="status && status.status === 'warning' && $route.name !== 'project_settings'"
                    color="error"
                    border="left"
                    class="mb-4 status-alert"
                    colored-border
                >
                    <div class="d-sm-flex align-center" style="gap: 8px">
                        <template v-if="status.error">
                            {{ status.error }}
                        </template>
                        <template v-else-if="status.prometheus.status !== 'ok'">
                            <div class="flex-grow-1 mb-3 mb-sm-0">
                                {{ status.prometheus.message }}
                                <div v-if="status.prometheus.error" class="mt-1" style="font-size: 14px">
                                    {{ status.prometheus.error }}
                                </div>
                            </div>
                            <v-btn
                                v-if="status.prometheus.action === 'configure'"
                                outlined
                                :to="{ name: 'project_settings', params: { tab: 'prometheus' } }"
                            >
                                <template v-if="status.prometheus.error"> Review the configuration </template>
                                <template v-else> Configure </template>
                            </v-btn>
                            <v-btn v-if="status.prometheus.action === 'wait'" outlined @click="refresh">refresh</v-btn>
                        </template>
                        <template v-else-if="status.node_agent.status !== 'ok'">
                            <div class="flex-grow-1 mb-3 mb-sm-0">
                                No metrics found. If you just installed shards and node-agent, please wait a couple minutes for it to collect data.
                                <br />
                                If you haven't installed node-agent, please do so now.
                            </div>
                            <AgentInstallation outlined>Install node-agent</AgentInstallation>
                        </template>
                        <template v-else-if="status.kube_state_metrics && status.kube_state_metrics.status !== 'ok'">
                            <div class="flex-grow-1 mb-3 mb-sm-0">
                                It looks like you use Kubernetes, so shards requires <b>kube-state-metrics</b>
                                to combine individual containers into applications.
                            </div>
                            <v-btn outlined :to="{ name: 'project_settings' }">Install kube-state-metrics</v-btn>
                        </template>
                        <template v-else-if="cloudWarning">
                            <div class="flex-grow-1 mb-3 mb-sm-0">{{ cloudWarning.name }} integration: {{ cloudWarning.message }}</div>
                            <v-btn outlined :to="{ name: 'project_settings', params: { tab: 'clouds' } }">Review the configuration</v-btn>
                        </template>
                    </div>
                </v-alert>

                <Welcome v-if="$route.name === 'index' && user && !projects.length" :user="user" />

                <router-view v-else-if="$route.name !== 'index'" />

                <ChangePassword v-if="user" v-model="changePassword" />
                <ApiKeys v-if="user" v-model="apiKeys" :user="user" />

                <Search v-if="search" v-model="search" />
            </v-container>
        </v-main>
    </v-app>
</template>

<script>
import Welcome from '@/views/Welcome.vue';
import Search from './views/Search.vue';
import ThemeSelector from './components/ThemeSelector.vue';
import TopBar from './components/TopBar.vue';
import AgentInstallation from './views/AgentInstallation.vue';
import ChangePassword from './views/auth/ChangePassword.vue';
import ApiKeys from './views/auth/ApiKeys.vue';
import { views } from '@/views/Views.vue';
import { repoUrl } from '@/utils/utils';

const groups = [
    { id: 'health', name: 'Health' },
    { id: 'explore', name: 'Explore' },
    { id: 'infrastructure', name: 'Infrastructure' },
];

export default {
    components: { Welcome, Search, ThemeSelector, TopBar, AgentInstallation, ChangePassword, ApiKeys },

    provide() {
        const vm = this;
        return {
            // shell exposes the layout state to the top bar rendered by the views.
            shell: {
                get user() {
                    return vm.user;
                },
                get projects() {
                    return vm.projects;
                },
                get project() {
                    return vm.project;
                },
                get mobile() {
                    return vm.mobile;
                },
                get mac() {
                    return vm.mac;
                },
                get alertsCount() {
                    return vm.alertsCount;
                },
                get alertsSeverity() {
                    return vm.alertsMaxSeverity;
                },
                openSearch() {
                    vm.search = true;
                },
                toggleDrawer() {
                    vm.drawer = !vm.drawer;
                },
            },
        };
    },

    data() {
        return {
            user: null,
            context: this.$api.context,
            changePassword: false,
            apiKeys: false,
            collapsed: !!this.$storage.local('menu-collapsed'),
            drawer: !this.$vuetify.breakpoint.mobile,
            search: false,
        };
    },

    mounted() {
        this.$events.watch(this, this.getUser, 'projects');
        this.getUser();
        window.addEventListener('keydown', this.searchListener);
    },

    beforeDestroy() {
        window.removeEventListener('keydown', this.searchListener);
    },

    computed: {
        cloudWarning() {
            return (this.status?.clouds || []).find((c) => c.status === 'warning');
        },
        projects() {
            if (!this.user) {
                return [];
            }
            return this.user.projects || [];
        },
        project() {
            const id = this.$route.params.projectId;
            if (!id) {
                return null;
            }
            return this.projects.find((p) => p.id === id);
        },
        status() {
            return this.project ? this.context.status : null;
        },
        view() {
            return this.$route.params.view;
        },
        views() {
            return views;
        },
        repoUrl() {
            return repoUrl;
        },
        mobile() {
            return this.$vuetify.breakpoint.mobile;
        },
        navGroups() {
            return groups
                .map((g) => ({
                    ...g,
                    items: Object.entries(this.views)
                        .filter(([, v]) => v.group === g.id)
                        .map(([id, v]) => ({ id, ...v, ...this.navBadges(id) })),
                }))
                .filter((g) => g.items.length);
        },
        pageTitle() {
            switch (this.$route.name) {
                case 'project_settings':
                    return 'Settings';
                case 'project_new':
                    return 'New project';
                case 'mcp-consent':
                    return 'Authorize access';
                case 'index':
                    return this.user && !this.projects.length ? 'Welcome' : '';
                default:
                    return '';
            }
        },
        initials() {
            const name = (this.user && this.user.name) || '';
            const parts = name.split(/[\s._@-]+/).filter(Boolean);
            return ((parts[0] || '?')[0] + (parts[1] ? parts[1][0] : '')).toUpperCase();
        },
        menu() {
            return !this.$route.meta.anonymous;
        },
        mac() {
            return /Mac|iPod|iPhone|iPad/.test(navigator.platform);
        },
        incidentsCount() {
            return Object.values(this.context.incidents).reduce((acc, current) => {
                return acc + current;
            }, 0);
        },
        alertsCount() {
            return Object.values(this.context.alerts).reduce((acc, current) => {
                return acc + current;
            }, 0);
        },
        alertsMaxSeverity() {
            if (this.context.alerts.critical > 0) {
                return 'critical';
            }
            if (this.context.alerts.warning > 0) {
                return 'warning';
            }
            return null;
        },
        kubernetesIssues() {
            const f = this.context.fluxcd;
            const a = this.context.argocd;
            return (f ? f.issues : 0) + (a ? a.issues : 0);
        },
    },

    watch: {
        $route: {
            handler(curr, prev) {
                this.getUser();
                if (curr.name === 'overview' && !this.views[curr.params.view]) {
                    this.$router.replace({ params: { view: 'applications' } }).catch((err) => err);
                    return;
                }
                if (!prev) {
                    return;
                }
                if (this.mobile) {
                    this.drawer = false;
                }
                if (
                    curr.query.from !== prev.query.from ||
                    curr.query.to !== prev.query.to ||
                    curr.query.incident !== prev.query.incident ||
                    curr.query.alert !== prev.query.alert
                ) {
                    this.$events.emit('refresh');
                }
            },
            immediate: true,
        },
        '$route.params.projectId'(v) {
            this.$events.emit('refresh');
            this.lastProject(v);
        },
        mobile(v) {
            this.drawer = !v;
        },
    },

    methods: {
        navBadges(id) {
            const badges = [];
            let dot = '';
            switch (id) {
                case 'incidents':
                    if (this.incidentsCount) {
                        badges.push({ class: 'danger', value: this.incidentsCount });
                        dot = 'danger';
                    }
                    break;
                case 'alerts':
                    if (this.context.alerts.critical) {
                        badges.push({ class: 'danger', value: this.context.alerts.critical });
                    }
                    if (this.context.alerts.warning) {
                        badges.push({ class: 'warning', value: this.context.alerts.warning });
                    }
                    if (this.alertsMaxSeverity) {
                        dot = this.alertsMaxSeverity === 'critical' ? 'danger' : 'warning';
                    }
                    break;
                case 'kubernetes':
                    if (this.kubernetesIssues) {
                        badges.push({ class: 'warning', value: this.kubernetesIssues });
                        dot = 'warning';
                    }
                    break;
            }
            return { badges, dot };
        },
        getMenuQuery(viewId) {
            switch (viewId) {
                case 'incidents':
                    return { ...this.$utils.contextQuery(), incident: undefined };
                case 'alerts':
                    return { ...this.$utils.contextQuery(), alert: undefined };
                case 'logs':
                    return { ...this.$utils.contextQuery(), query: undefined };
                case 'kubernetes':
                    return { ...this.$utils.contextQuery(), query: undefined };
                default:
                    return this.$utils.contextQuery();
            }
        },
        getUser() {
            if (this.$route.meta.anonymous) {
                return;
            }
            this.$api.user(null, (data, error) => {
                if (error) {
                    this.user = null;
                    return;
                }
                this.user = data;
                if (this.$route.name === 'index' && this.projects.length) {
                    let id = this.projects[0].id;
                    const lastId = this.lastProject();
                    if (lastId && this.projects.find((p) => p.id === lastId)) {
                        id = lastId;
                    }
                    this.$router.replace({ name: 'overview', params: { projectId: id } });
                }
            });
        },
        lastProject(id) {
            return this.$storage.local('last-project', id);
        },
        refresh() {
            this.$events.emit('refresh');
        },
        toggleCollapsed() {
            this.collapsed = !this.collapsed;
            this.$storage.local('menu-collapsed', this.collapsed);
        },
        searchListener(e) {
            if (this.project && (e.metaKey || e.ctrlKey) && e.key === 'k') {
                e.preventDefault();
                this.search = true;
            }
        },
    },
};
</script>

<style scoped>
.content {
    padding: 20px 24px 48px;
}
@media (max-width: 600px) {
    .content {
        padding: 16px 12px 40px;
    }
}

/* ---------- rail ---------- */
.rail.v-navigation-drawer {
    background: var(--surface) !important;
    border-right: 1px solid var(--border);
    color: var(--text-1);
}
.rail-inner {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
}
.brand {
    display: flex;
    align-items: center;
    gap: 10px;
    height: 52px;
    padding: 0 19px;
    flex: none;
    color: var(--text-1) !important;
    text-decoration: none !important;
}
.brand-icon {
    flex: none;
    border-radius: 7px;
}
.wordmark {
    font-size: 17px;
    font-weight: 600;
    letter-spacing: 0.02em;
    line-height: 1;
    white-space: nowrap;
}
.rail-nav {
    overflow-y: auto;
    overflow-x: hidden;
    padding: 4px 8px;
    min-height: 0;
}
.rail-group + .rail-group {
    margin-top: 10px;
}
.rail-title {
    font: 600 10px var(--mono);
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--text-3);
    padding: 6px 8px 4px;
    white-space: nowrap;
}
.rail-item {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    height: 34px;
    padding: 0 8px;
    border-radius: var(--r-sm);
    color: var(--text-2) !important;
    font-weight: 500;
    font-size: 13.5px;
    position: relative;
    text-align: left;
    text-decoration: none !important;
    cursor: pointer;
    white-space: nowrap;
}
.rail-item + .rail-item {
    margin-top: 1px;
}
.rail-item:hover {
    background: var(--hover);
    color: var(--text-1) !important;
}
.rail-item.active {
    background: var(--accent-3);
    color: var(--accent-12) !important;
}
.rail-item.active::before {
    content: '';
    position: absolute;
    left: -8px;
    top: 8px;
    bottom: 8px;
    width: 3px;
    border-radius: 0 3px 3px 0;
    background: var(--accent-9);
}
.tile {
    position: relative;
    flex: none;
    width: 22px;
    height: 22px;
    border-radius: 6px;
    display: grid;
    place-items: center;
}
.tile .v-icon {
    color: inherit !important;
}
.tile-plain {
    color: var(--text-2);
}
.tile-accent {
    background: var(--accent-9);
    color: var(--accent-on);
}
.tile-danger {
    background: var(--danger-9);
    color: var(--danger-on);
}
.tile-warning {
    background: var(--warning-9);
    color: var(--warning-on);
}
.tile-success {
    background: var(--success-9);
    color: #fff;
}
.tile-info {
    background: var(--info-9);
    color: var(--info-on);
}
.tile-cyan {
    background: var(--cyan-9);
    color: #fff;
}
.tile-slate {
    background: var(--slate-9);
    color: var(--slate-on);
}
.tile-purple {
    background: var(--purple-9);
    color: var(--purple-on);
}
.tile-pink {
    background: var(--pink-9);
    color: #fff;
}
.tile-dot {
    display: none;
    position: absolute;
    top: -3px;
    right: -3px;
    width: 9px;
    height: 9px;
    border-radius: 50%;
    border: 2px solid var(--surface);
}
.tile-dot.danger {
    background: var(--danger-9);
}
.tile-dot.warning {
    background: var(--warning-9);
}
.label {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
}
.badges {
    display: inline-flex;
    gap: 3px;
}
.badge {
    font-size: 11px;
    font-weight: 600;
    min-width: 18px;
    height: 18px;
    padding: 0 5px;
    border-radius: 9px;
    display: inline-grid;
    place-items: center;
    background: var(--gray-4);
    color: var(--text-1);
    font-variant-numeric: tabular-nums;
}
.badge.danger {
    background: var(--danger-9);
    color: var(--danger-on);
}
.badge.warning {
    background: var(--warning-9);
    color: var(--warning-on);
}
.rail-spacer {
    flex: 1;
}
.rail-foot {
    border-top: 1px solid var(--border);
    padding: 8px;
    flex: none;
}
.user {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    margin-top: 6px;
    padding: 6px;
    border-radius: var(--r-sm);
    text-align: left;
    color: var(--text-1);
}
.user:hover {
    background: var(--hover);
}
.avatar {
    flex: none;
    width: 28px;
    height: 28px;
    border-radius: 50%;
    display: grid;
    place-items: center;
    font-weight: 600;
    font-size: 11px;
    background: var(--accent-4);
    color: var(--accent-12);
}
.user-text {
    display: flex;
    flex-direction: column;
    min-width: 0;
    flex: 1;
    line-height: 1.25;
}
.user-name {
    font-weight: 600;
    font-size: 13px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}
.user-role {
    font-size: 11px;
    color: var(--text-2);
}
.user-more {
    color: var(--text-3) !important;
}

/* collapsed (icons only) */
.rail.mini .brand {
    padding: 0 19px;
}
.rail.mini .wordmark,
.rail.mini .label,
.rail.mini .badges,
.rail.mini .user-text,
.rail.mini .user-more {
    display: none;
}
.rail.mini .rail-title {
    visibility: hidden;
    height: 8px;
    padding: 0;
}
.rail.mini .rail-item {
    justify-content: center;
    padding: 0;
}
.rail.mini .tile-dot {
    display: block;
}
.rail.mini .user {
    justify-content: center;
    padding: 6px 0;
}
</style>
