<template>
    <v-app-bar app flat :height="52" class="topbar">
        <div class="topbar-inner">
            <button v-if="shell.mobile" type="button" class="icon-btn" aria-label="Open navigation" @click="shell.toggleDrawer()">
                <v-icon size="18">mdi-menu</v-icon>
            </button>

            <nav class="crumbs" aria-label="Breadcrumbs">
                <v-menu v-if="shell.user" offset-y :close-on-content-click="true">
                    <template #activator="{ on, attrs }">
                        <button type="button" class="crumb project" v-bind="attrs" v-on="on" :title="projectName">
                            <v-icon size="15" class="mr-1">mdi-hexagon-multiple-outline</v-icon>
                            <span class="crumb-text">{{ projectName }}</span>
                            <v-icon size="16">mdi-chevron-down</v-icon>
                        </button>
                    </template>
                    <v-list dense class="py-1">
                        <v-subheader>Projects</v-subheader>
                        <v-list-item
                            v-for="p in shell.projects"
                            :key="p.id"
                            :to="{ name: 'overview', params: { projectId: p.id } }"
                            :input-value="shell.project && p.id === shell.project.id"
                        >
                            {{ p.name }}
                        </v-list-item>
                        <v-list-item v-if="!shell.user.readonly" :to="{ name: 'project_new' }" exact>
                            <v-icon small class="mr-1">mdi-plus</v-icon> new project
                        </v-list-item>
                        <v-list-item v-else-if="!shell.projects.length" disabled>no projects available</v-list-item>
                    </v-list>
                </v-menu>
                <template v-if="$slots.crumbs || title">
                    <span v-if="shell.user" class="sep">/</span>
                    <div class="crumb current">
                        <slot name="crumbs">{{ title }}</slot>
                    </div>
                </template>
            </nav>

            <v-spacer />

            <template v-if="shell.project">
                <button v-if="!shell.mobile" type="button" class="search" @click="shell.openSearch()">
                    <v-icon size="15">mdi-magnify</v-icon>
                    <span class="search-text">Go to app or node…</span>
                    <span class="kbd">{{ shell.mac ? '⌘' : 'Ctrl+' }}K</span>
                </button>
                <button v-else type="button" class="icon-btn" aria-label="Search" @click="shell.openSearch()">
                    <v-icon size="17">mdi-magnify</v-icon>
                </button>
            </template>

            <slot name="actions" />

            <button
                type="button"
                class="icon-btn"
                :title="theme.dark ? 'Switch to light theme' : 'Switch to dark theme'"
                :aria-label="theme.dark ? 'Switch to light theme' : 'Switch to dark theme'"
                @click="toggleTheme"
            >
                <v-icon size="17">{{ theme.dark ? 'mdi-white-balance-sunny' : 'mdi-weather-night' }}</v-icon>
            </button>

            <router-link
                v-if="shell.project"
                class="icon-btn bell"
                :to="{ name: 'overview', params: { projectId: shell.project.id, view: 'alerts' }, query: $utils.contextQuery() }"
                :title="alertsTitle"
                :aria-label="alertsTitle"
            >
                <v-icon size="17">mdi-bell-outline</v-icon>
                <span v-if="shell.alertsSeverity" class="bell-dot" :class="shell.alertsSeverity" />
            </router-link>
        </div>
    </v-app-bar>
</template>

<script>
import { state as themeState, toggle } from '@/utils/theme';

export default {
    inject: ['shell'],

    props: {
        title: String,
    },

    computed: {
        theme() {
            return themeState;
        },
        projectName() {
            return this.shell.project ? this.shell.project.name : 'choose a project';
        },
        alertsTitle() {
            const n = this.shell.alertsCount;
            return n ? `${n} firing ${this.$pluralize('alert', n)}` : 'Alerts';
        },
    },

    methods: {
        toggleTheme() {
            toggle();
        },
    },
};
</script>

<style scoped>
.topbar.v-app-bar.v-app-bar--fixed {
    background: var(--surface) !important;
    border-bottom: 1px solid var(--border) !important;
    box-shadow: none !important;
}
.topbar:deep(.v-toolbar__content) {
    padding: 0 20px;
}
.topbar-inner {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    min-width: 0;
}
.crumbs {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    color: var(--text-2);
    font-size: 14px;
}
.crumb {
    display: inline-flex;
    align-items: center;
    min-width: 0;
    white-space: nowrap;
}
.crumb.project {
    height: 30px;
    padding: 0 6px 0 8px;
    border-radius: var(--r-sm);
    color: var(--text-2);
    font-weight: 500;
    max-width: 220px;
}
.crumb.project:hover {
    background: var(--hover);
    color: var(--text-1);
}
.crumb-text {
    overflow: hidden;
    text-overflow: ellipsis;
}
.crumb.current {
    color: var(--text-1);
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
}
.crumb.current:deep(a) {
    color: var(--text-2);
    font-weight: 500;
}
.sep {
    color: var(--text-3);
}
.search {
    display: flex;
    align-items: center;
    gap: 8px;
    height: 32px;
    width: 260px;
    padding: 0 8px 0 10px;
    border: 1px solid var(--border);
    border-radius: var(--r-sm);
    background: var(--surface-2);
    color: var(--text-2);
    font-size: 13px;
    cursor: text;
}
.search:hover {
    border-color: var(--border-strong);
}
.search .v-icon {
    color: var(--text-2);
}
.search-text {
    flex: 1;
    text-align: left;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}
.kbd {
    font: 500 10.5px var(--mono);
    padding: 1px 5px;
    border-radius: 4px;
    border: 1px solid var(--border);
    background: var(--surface);
    color: var(--text-2);
}
.icon-btn {
    position: relative;
    flex: none;
    width: 32px;
    height: 32px;
    display: grid;
    place-items: center;
    border-radius: var(--r-sm);
    border: 1px solid var(--border);
    background: var(--surface);
    color: var(--text-2);
    cursor: pointer;
}
.icon-btn:hover {
    background: var(--hover);
    text-decoration: none !important;
}
.icon-btn .v-icon {
    color: var(--text-2);
}
.bell-dot {
    position: absolute;
    top: 5px;
    right: 6px;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    border: 1.5px solid var(--surface);
}
.bell-dot.critical {
    background: var(--danger-9);
}
.bell-dot.warning {
    background: var(--warning-9);
}
@media (max-width: 600px) {
    .topbar:deep(.v-toolbar__content) {
        padding: 0 12px;
    }
    .crumb.project .crumb-text,
    .bell {
        display: none;
    }
}
</style>
