<template>
    <Views :loading="loading" :error="error">
        <NoData v-if="!loading && !applications.length" />

        <div v-else class="d-flex align-start mb-4" style="gap: 12px">
            <ApplicationFilter
                ref="filter"
                :applications="applications"
                :autoSelectNamespaceThreshold="maxApplications"
                :highlightSearch="mode === 'graph'"
                storageKey="service-map-filter"
                :defaultCategories="defaultCategories"
                :searchInfo="mode === 'graph' && filtered.length ? searchInfo : ''"
                @filter="setFilter"
                @search="query = $event"
                @search-keydown="searchKeyDown"
                class="flex-grow-1"
            />
            <v-btn-toggle v-model="mode" mandatory dense>
                <v-tooltip v-for="m in modeButtons" :key="m.value" bottom transition="none">
                    <template #activator="{ on, attrs }">
                        <v-btn :value="m.value" height="40" v-bind="attrs" v-on="on" :aria-label="m.label">
                            <v-icon small>{{ m.icon }}</v-icon>
                        </v-btn>
                    </template>
                    <v-card class="px-2">{{ m.label }}</v-card>
                </v-tooltip>
            </v-btn-toggle>
        </div>

        <div v-if="tooManyApplications" class="text-center red--text mt-5">
            Too many applications ({{ tooManyApplications }}) to render. Please choose a different category or namespace.
        </div>

        <div v-if="collapsible.length" class="groups-bar mb-2">
            <span class="groups-bar-label">Groups</span>
            <button
                v-for="g in collapsible"
                :key="g.key"
                type="button"
                class="group-chip"
                :class="{ collapsed: g.collapsed, muted: g.muted }"
                :aria-pressed="String(!g.collapsed)"
                :title="(g.collapsed ? 'expand ' : 'collapse ') + g.label"
                @click="toggleGroup(g.key)"
            >
                <v-icon x-small>{{ g.collapsed ? 'mdi-chevron-right' : 'mdi-chevron-down' }}</v-icon>
                {{ g.label }}
                <span class="count">{{ g.count }}</span>
            </button>
        </div>

        <ServiceMapGraph
            v-if="mode === 'graph' && filtered.length"
            ref="graph"
            :applications="displayed"
            :categories="categories"
            :query="query"
            :selected="$route.query.app"
            @select="setSelected"
            @search-info="searchInfo = $event"
            @toggle-group="toggleGroup"
        />

        <div v-if="mode === 'columns'" class="applications" v-on-resize="calc" @scroll="calc">
            <div
                v-for="apps in levels"
                class="level"
                style="z-index: 1"
                :style="{ rowGap: 200 / apps.length + 'px', maxWidth: 100 / levels.length + '%' }"
            >
                <div v-for="a in apps" style="text-align: center">
                    <div
                        :ref="a.id"
                        class="app"
                        :class="{ selected: a.hi(hi), muted: a.muted, external: a.external }"
                        @mouseenter="hi = a.id"
                        @mouseleave="hi = null"
                    >
                        <div class="d-flex align-center">
                            <div class="flex-grow-1 name">
                                <a v-if="a.collapsed" href="#" class="collapsed-app" @click.prevent="toggleGroup(a.groupKey)">
                                    <v-icon small>mdi-arrow-expand-all</v-icon> {{ a.display_name }} <span class="count">{{ a.members }}</span>
                                </a>
                                <router-link
                                    v-else
                                    :to="{ name: 'overview', params: { view: 'applications', id: a.id }, query: $utils.contextQuery() }"
                                >
                                    <AppHealth :app="a" />
                                </router-link>
                            </div>
                            <div v-if="!a.collapsed">
                                <AppPreferences :app="a" :categories="categories" />
                            </div>
                            <AppIcon v-if="!a.collapsed" :icon="a.icon" />
                        </div>
                        <Labels
                            :labels="a.labels"
                            :cluster="$api.context.multicluster ? a.cluster : ''"
                            :hideLabels="hideLabels"
                            class="d-none d-sm-block label"
                        />
                    </div>
                </div>
            </div>
            <svg :style="{ zIndex: hi ? 2 : 0 }">
                <defs>
                    <template v-for="s in ['unknown', 'ok', 'warning', 'critical']">
                        <marker
                            :id="`marker-${s}`"
                            class="marker"
                            :class="s"
                            viewBox="0 0 10 10"
                            refX="10"
                            refY="5"
                            :markerWidth="10"
                            :markerHeight="10"
                            markerUnits="userSpaceOnUse"
                            orient="auto-start-reverse"
                        >
                            <path d="M 0 3 L 10 5 L 0 7 z" />
                        </marker>
                    </template>
                </defs>

                <template v-for="a in arrows">
                    <path v-if="a.dd" :d="a.dd" class="arrow" :class="a.status" />
                    <path :d="a.d" class="arrow" :class="a.status" :stroke-opacity="a.hi ? 1 : 0.7" :marker-end="`url(#marker-${a.status})`" />
                </template>
            </svg>
            <template v-for="a in arrows">
                <div v-if="a.stats && a.hi" class="stats" :style="{ top: a.stats.y + 'px', left: a.stats.x + 'px', zIndex: 3 }">
                    <div v-for="i in a.stats.items">{{ i }}</div>
                </div>
            </template>
        </div>
    </Views>
</template>

<script>
import Views from '@/views/Views.vue';
import Labels from '@/components/Labels.vue';
import AppHealth from '@/components/AppHealth.vue';
import AppIcon from '@/components/AppIcon.vue';
import ApplicationFilter from '@/components/ApplicationFilter.vue';
import AppPreferences from '@/components/AppPreferences.vue';
import NoData from '@/components/NoData.vue';
import ServiceMapGraph from '@/components/ServiceMapGraph.vue';
import { collapseGroups } from '@/utils/serviceMapGroups';

const modeButtons = [
    { value: 'columns', label: 'tiers view', icon: 'mdi-view-column-outline' },
    { value: 'graph', label: 'topology view', icon: 'mdi-graph-outline' },
];

function findBackLinks(index, a, discovered, finished, found) {
    if (!a) {
        return;
    }
    discovered.add(a.id);
    for (const u of a.upstreams) {
        if (discovered.has(u.id)) {
            found.add(a.id + '->' + u.id);
            continue;
        }
        if (!finished.has(u.id)) {
            findBackLinks(index, index.get(u.id), discovered, finished, found);
        }
    }
    discovered.delete(a.id);
    finished.add(a.id);
}

function calcLevel(index, a, level, backLinks) {
    if (!a) {
        return;
    }
    if (a.level === undefined || level > a.level) {
        a.level = level;
    }
    for (const u of a.upstreams) {
        const l = a.id + '->' + u.id;
        if (backLinks.has(l)) {
            continue;
        }
        calcLevel(index, index.get(u.id), level + 1, backLinks);
    }
}

export default {
    components: { Views, NoData, AppPreferences, ApplicationFilter, AppHealth, Labels, AppIcon, ServiceMapGraph },

    data() {
        const mode = this.$storage.local('service-map-mode');
        return {
            applications: [],
            categories: [],
            categoryModes: {},
            groupState: this.$storage.local('service-map-groups') || {},
            loading: false,
            error: '',
            levels: [],
            arrows: [],
            hi: null,
            filter: new Set(),
            tooManyApplications: 0,
            filtered: [],
            query: '',
            searchInfo: '',
            mode: this.$route.query.app ? 'graph' : modeButtons.some((m) => m.value === mode) ? mode : 'columns',
        };
    },

    mounted() {
        this.get();
        this.$events.watch(this, this.get, 'refresh');
        this.calc();
        window.addEventListener('keydown', this.keyDown);
    },

    beforeDestroy() {
        window.removeEventListener('keydown', this.keyDown);
    },

    watch: {
        applications() {
            this.calc();
        },
        hi(hi) {
            this.highlightArrows(hi);
        },
        selectedCategories() {
            this.calc();
        },
        query(query) {
            if (query) {
                this.setSelected(null);
            }
        },
        mode(mode) {
            this.$storage.local('service-map-mode', mode);
            if (mode === 'columns') {
                this.setSelected(null);
                this.$nextTick(this.calc);
            }
        },
    },
    computed: {
        modeButtons() {
            return modeButtons;
        },
        maxApplications() {
            return 1000;
        },
        hideLabels() {
            return this.levels.some((l) => l.length >= 15);
        },
        defaultCategories() {
            return Object.keys(this.categoryModes).filter((c) => this.categoryModes[c] !== 'hidden');
        },
        grouping() {
            return collapseGroups(this.filtered, this.categoryModes, this.groupState);
        },
        displayed() {
            return this.grouping.apps;
        },
        collapsible() {
            return this.grouping.groups;
        },
    },
    methods: {
        get() {
            this.loading = true;
            this.error = '';
            this.$api.getOverview('map', '', (data, error) => {
                this.loading = false;
                if (error) {
                    this.error = error;
                    return;
                }
                this.categoryModes = (data.service_map && data.service_map.category_modes) || {};
                this.applications = data.map || [];
                this.categories = data.categories || [];
            });
        },
        toggleGroup(key) {
            const g = this.collapsible.find((g) => g.key === key);
            if (!g) {
                return;
            }
            this.groupState = { ...this.groupState, [key]: g.collapsed ? 'expanded' : 'collapsed' };
            this.$storage.local('service-map-groups', this.groupState);
            this.$nextTick(this.calc);
        },
        setSelected(id) {
            if (id && this.query) {
                this.$refs.filter.clearSearch();
            }
            if ((this.$route.query.app || null) === id) {
                return;
            }
            this.$router.replace({ query: { ...this.$route.query, app: id || undefined } }).catch((err) => err);
        },
        searchKeyDown(e) {
            if (this.mode !== 'graph') {
                return;
            }
            if (e.key === 'Enter') {
                e.preventDefault();
                if (this.$refs.graph && this.$refs.graph.goToMatches()) {
                    e.target.blur();
                }
            } else if (e.key === 'Escape' && this.query) {
                this.$refs.filter.clearSearch();
            } else if (e.key === 'Escape') {
                e.target.blur();
            }
        },
        keyDown(e) {
            const input = this.mode === 'graph' && this.$refs.filter && this.$refs.filter.searchInput();
            if (input && (e.metaKey || e.ctrlKey) && e.key === 'f' && e.target !== input) {
                e.preventDefault();
                input.focus();
                input.select();
            }
        },
        setFilter(filter) {
            this.filter = filter;
            this.calc();
        },
        calc() {
            if (!this.applications) {
                return;
            }
            const index = new Map();
            this.applications.forEach((a) => {
                index.set(a.id, a);
            });
            this.tooManyApplications = 0;
            const filter = (a) => index.get(a.id) && this.filter.has(a.id);
            const filtered = this.applications.filter(filter);
            if (filtered.length > this.maxApplications) {
                this.tooManyApplications = filtered.length;
                this.filtered = [];
                this.levels = [];
                this.arrows = [];
                return;
            }
            this.filtered = filtered;
            if (this.mode !== 'columns') {
                return;
            }
            const shown = collapseGroups(filtered, this.categoryModes, this.groupState).apps;
            const shownIds = new Set(shown.map((a) => a.id));
            const shownFilter = (a) => shownIds.has(a.id);
            const applications = shown.map((a) => ({ ...a }));
            applications.forEach((a) => {
                a.name = a.display_name || this.$utils.appId(a.id).name;
                a.level = 0;
                a.upstreams = a.upstreams.filter(shownFilter);
                a.upstreams.sort((u1, u2) => u1.id.localeCompare(u2.id));
                a.downstreams = a.downstreams.filter(shownFilter);
                a.hi = (hi) => Array.of(a, ...a.upstreams, ...a.downstreams).some((aa) => aa.id === hi);
            });
            applications.sort((a, b) => a.name.localeCompare(b.name));
            this.calcLevels(applications);
            requestAnimationFrame(() => this.calcArrows(applications));
        },
        calcLevels(applications) {
            if (applications.length === 0) {
                this.levels = [];
                return;
            }
            const index = new Map();
            applications.forEach((a) => {
                index.set(a.id, a);
            });
            const backLinks = new Set();
            applications.forEach((a) => {
                findBackLinks(index, a, new Set(), new Set(), backLinks);
            });
            applications.forEach((a) => {
                a.downstreams = a.downstreams.filter((d) => !backLinks.has(d.id + '->' + a.id));
            });

            const roots = applications.filter((a) => a.downstreams.length === 0);
            roots.forEach((a) => {
                calcLevel(index, a, 0, backLinks);
            });

            const depth = Math.max(...applications.map((a) => a.level));
            const levels = Array.from({ length: depth + 1 }, () => []);
            applications.forEach((a) => {
                let l = a.level;
                if (a.upstreams.length === 0 && a.downstreams.length === 0) {
                    l = depth;
                } else if (a.downstreams.length === 0) {
                    l = 0;
                } else if (a.upstreams.length === 0) {
                    l = depth;
                }
                levels[l].push(a);
            });
            this.levels = levels;
        },
        calcArrows(applications) {
            if (!applications.length) {
                this.arrows = [];
                return;
            }
            const getRect = (ref) => {
                const el = this.$refs[ref] && (this.$refs[ref][0] || this.$refs[ref]);
                if (!el) {
                    return null;
                }
                return { top: el.offsetTop, left: el.offsetLeft, width: el.offsetWidth, height: el.offsetHeight };
            };
            const arrows = [];
            applications.forEach((app) => {
                app.upstreams.forEach((u) => {
                    const a = {
                        src: app.id,
                        dst: u.id,
                        status: u.status,
                        w: u.weight || 0,
                    };
                    const s = getRect(a.src);
                    const d = getRect(a.dst);
                    if (!s || !d) {
                        return;
                    }
                    arrows.push(a);

                    a.x1 = s.left + s.width;
                    a.y1 = s.top + s.height / 2;
                    a.x2 = d.left;
                    a.y2 = d.top + d.height / 2;
                    if (a.x1 > a.x2) {
                        a.x1 = s.left;
                        a.x2 = d.left + d.width;
                    }
                    a.d = `M${a.x1},${a.y1} L${a.x2},${a.y2}`;

                    if (u.stats && u.stats.length) {
                        a.stats = { x: (a.x2 + a.x1) / 2 - 20, y: (a.y2 + a.y1) / 2 - (u.stats.length * 12) / 2, items: u.stats };
                    }
                });
            });
            this.arrows = arrows;
        },
        highlightArrows(hiApp) {
            this.arrows.forEach((a) => {
                a.hi = hiApp && (a.src === hiApp || a.dst === hiApp);
                a.dd = '';
            });
            if (!hiApp) {
                return;
            }
            const hiArrows = this.arrows.filter((a) => a.hi);
            const maxW = Math.max(...hiArrows.map((a) => a.w));
            if (!maxW) {
                return;
            }
            hiArrows.forEach((a) => {
                if (!a.w) {
                    return;
                }
                const w = (3 * a.w) / maxW;
                const r = w / 2 + ((a.y2 - a.y1) ** 2 + (a.x2 - a.x1) ** 2) / (8 * w);
                a.dd = `M${a.x1},${a.y1} A${r},${r} 0,0,0 ${a.x2},${a.y2} A${r},${r} 0,0,0 ${a.x1},${a.y1}`;
            });
        },
    },
};
</script>

<style scoped>
.applications {
    position: relative;
    display: flex;
    padding: 10px 0;
    overflow-x: auto;
    gap: 16px;
}
.level {
    min-width: 120px;
    flex-grow: 1;
    display: flex;
    flex-direction: column;
    justify-content: space-around;
    /*row-gap: 32px;*/
}
.app {
    max-width: 100%;
    border: 1px solid #bdbdbd;
    border-radius: 3px;
    white-space: nowrap;
    padding: 4px 8px;
    background-color: var(--background-color);
    display: inline-flex;
    flex-direction: column;
    line-height: 1.1;
    text-align: left;
}
.app.muted {
    opacity: 0.6;
    border-style: dashed;
}
.app.external {
    border-style: dashed;
}
.collapsed-app {
    font-weight: 500;
}
.collapsed-app .count,
.group-chip .count {
    font-size: 11px;
    color: var(--text-3);
    margin-left: 2px;
}
.groups-bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
}
.groups-bar-label {
    font-size: 12px;
    color: var(--text-3);
    margin-right: 2px;
}
.group-chip {
    display: inline-flex;
    align-items: center;
    gap: 2px;
    height: 24px;
    padding: 0 8px 0 4px;
    border: 1px solid var(--border);
    border-radius: var(--r-sm);
    background: var(--surface);
    color: var(--text-1);
    font-size: 12px;
    cursor: pointer;
}
.group-chip:hover {
    background: var(--hover);
}
.group-chip:focus-visible {
    outline: 2px solid var(--focus);
    outline-offset: 1px;
}
.group-chip.collapsed {
    background: var(--surface-sunk);
    color: var(--text-2);
}
.group-chip.muted {
    border-style: dashed;
}
.app.selected {
    border: 1px solid var(--text-color);
    background-color: var(--background-color-hi);
}
.name {
    white-space: nowrap;
    display: inline-block;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
}

.label {
    margin-left: 14px;
}

svg {
    position: absolute;
    top: 0;
    left: 0;
    width: 100%;
    height: 100%;
    pointer-events: none; /* to allow interactions with html below */
    overflow: visible;
}
.arrow.unknown {
    fill: var(--status-unknown);
    stroke: var(--status-unknown);
    stroke-dasharray: 4;
}
.arrow.ok {
    fill: var(--text-3);
    stroke: var(--text-3);
}
.arrow.warning {
    fill: var(--status-warning);
    stroke: var(--status-warning);
    stroke-dasharray: 6;
    stroke-width: 1.5;
}
.arrow.critical {
    fill: var(--danger-9);
    stroke: var(--danger-9);
    stroke-dasharray: 6;
    stroke-width: 1.5;
}
.marker.unknown {
    fill: var(--status-unknown);
}
.marker.ok {
    fill: var(--text-3);
}
.marker.warning {
    fill: var(--status-warning);
}
.marker.critical {
    fill: var(--danger-9);
}

.stats {
    position: absolute;
    font-size: 12px;
    line-height: 12px;
    background-color: var(--background-color-hi);
    padding: 2px;
    border-radius: 2px;
    text-align: right;
    pointer-events: none;
}
</style>
