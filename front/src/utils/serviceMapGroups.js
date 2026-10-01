// shards fork: service map groups.
//
// Every application gets a group key: a category drawn "collapsed"/"muted" groups all its applications
// (the monitoring plane is one node), otherwise the compose project / namespace reported by the server.
// A collapsed group is replaced by one synthetic node whose edges are the union of its members' edges.

const statusRank = { unknown: 0, ok: 1, warning: 2, critical: 3 };
const worst = (a, b) => ((statusRank[a] || 0) >= (statusRank[b] || 0) ? a : b);

export function groupKey(app, categoryModes) {
    const mode = categoryModes[app.category];
    if (mode === 'collapsed' || mode === 'muted') {
        return 'category:' + app.category;
    }
    if (app.group) {
        return 'group:' + app.group;
    }
    return '';
}

function groupLabel(key) {
    return key.slice(key.indexOf(':') + 1);
}

export function groupNodeId(key) {
    return '~group:' + key;
}

function isExternal(id) {
    return id.split(':')[2] === 'ExternalService';
}

// collapseGroups returns the applications to draw (with group annotations and synthetic group nodes)
// and the list of groups that can be collapsed/expanded.
// state: {[groupKey]: 'collapsed'|'expanded'} overrides the defaults (collapsed for collapsed/muted categories).
export function collapseGroups(applications, categoryModes, state) {
    const keyOf = new Map();
    const groups = new Map();
    (applications || []).forEach((a) => {
        const key = groupKey(a, categoryModes || {});
        keyOf.set(a.id, key);
        if (!key) {
            return;
        }
        let g = groups.get(key);
        if (!g) {
            const mode = key.startsWith('category:') ? categoryModes[a.category] : 'expanded';
            const defaultCollapsed = mode === 'collapsed' || mode === 'muted';
            const s = state && state[key];
            g = {
                key,
                label: groupLabel(key),
                kind: key.startsWith('category:') ? 'category' : a.group_source || 'group',
                muted: mode === 'muted',
                collapsed: s ? s === 'collapsed' : defaultCollapsed,
                members: [],
            };
            groups.set(key, g);
        }
        g.members.push(a);
    });
    // a single-member product group is not worth a frame, but a collapsed category always is
    groups.forEach((g, key) => {
        if (g.members.length < 2 && g.kind !== 'category') {
            g.members.forEach((a) => keyOf.set(a.id, ''));
            groups.delete(key);
        }
    });

    const rep = (id) => {
        const key = keyOf.get(id);
        if (key && groups.get(key).collapsed) {
            return groupNodeId(key);
        }
        return id;
    };

    const res = [];
    const synthetic = new Map();
    groups.forEach((g) => {
        if (!g.collapsed) {
            return;
        }
        const node = {
            id: groupNodeId(g.key),
            display_name: g.label,
            category: g.members[0].category,
            status: g.members.reduce((s, a) => worst(s, a.status || 'unknown'), 'unknown'),
            icon: '',
            labels: {},
            indicators: [],
            upstreams: [],
            downstreams: [],
            collapsed: true,
            muted: g.muted,
            members: g.members.length,
            memberNames: g.members.map((a) => a.id.split(':').slice(3).join(':')).sort(),
            groupKey: g.key,
            groupLabel: g.label,
        };
        synthetic.set(node.id, node);
        res.push(node);
    });

    const merge = (list, link, target) => {
        let l = list.find((x) => x.id === target);
        if (!l) {
            l = { id: target, status: link.status || 'unknown', weight: 0, stats: link.stats || [], n: 0 };
            list.push(l);
        } else {
            l.status = worst(l.status, link.status || 'unknown');
            l.stats = [];
        }
        l.weight += link.weight || 0;
        l.n++;
    };

    (applications || []).forEach((a) => {
        const self = rep(a.id);
        const owner = synthetic.get(self);
        const out = owner || { ...a, upstreams: [], downstreams: [] };
        (a.upstreams || []).forEach((u) => {
            const t = rep(u.id);
            if (t !== self) {
                merge(out.upstreams, u, t);
            }
        });
        (a.downstreams || []).forEach((d) => {
            const t = rep(d.id);
            if (t !== self) {
                merge(out.downstreams, d, t);
            }
        });
        if (!owner) {
            const key = keyOf.get(a.id);
            if (key) {
                out.groupKey = key;
                out.groupLabel = groups.get(key).label;
                out.groupMuted = groups.get(key).muted;
            }
            out.external = isExternal(a.id);
            res.push(out);
        }
    });
    res.forEach((a) => {
        a.upstreams.forEach((l) => {
            if (l.n > 1) {
                l.stats = [`${l.n} connections`];
            }
            delete l.n;
        });
        a.downstreams.forEach((l) => delete l.n);
    });

    const list = [...groups.values()].map((g) => ({
        key: g.key,
        label: g.label,
        kind: g.kind,
        muted: g.muted,
        collapsed: g.collapsed,
        count: g.members.length,
    }));
    list.sort((a, b) => (a.kind === 'category') - (b.kind === 'category') || a.label.localeCompare(b.label));
    return { apps: res, groups: list };
}
