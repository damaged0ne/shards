const emptyJson = JSON.stringify({});

export const repoUrl = 'https://github.com/damaged0ne/shards';
const docsSections = ['costs', 'dashboards', 'gitops', 'logs', 'mcp', 'profiling', 'risks', 'tracing', 'inspections'];

// docsUrl maps a docs path (e.g. 'configuration/prometheus#remote-write') to the markdown source in the repository.
export function docsUrl(path) {
    let [p, hash] = (path || '').split('#');
    p = p.replace(/^\/+|\/+$/g, '');
    if (!p) {
        return `${repoUrl}/tree/main/docs/docs`;
    }
    if (docsSections.includes(p)) {
        p += '/overview';
    }
    return `${repoUrl}/blob/main/docs/docs/${p}.md${hash ? '#' + hash : ''}`;
}

export default class Utils {
    router = null;

    constructor(router) {
        this.router = router;
    }

    stateToUri(s) {
        const j = JSON.stringify(s);
        const hash = j === emptyJson ? undefined : '#' + encodeURIComponent(j);
        this.router.replace({ hash }).catch((err) => err);
    }

    stateFromUri() {
        const j = decodeURIComponent(this.router.currentRoute.hash.substring(1));
        if (!j) {
            return {};
        }
        try {
            return JSON.parse(j);
        } catch {
            this.router.replace({ hash: undefined });
            return {};
        }
    }

    appId(id) {
        const parts = id.split(':');
        return {
            cluster: parts[0],
            ns: parts[1] !== '_' ? parts[1] : '',
            kind: parts[2],
            name: parts[4] ? parts[3] + ':' + parts[4] : parts[3],
        };
    }

    nodeId(id) {
        const i = id.indexOf(':');
        if (i < 0) {
            return { cluster: '', name: id };
        }
        return {
            cluster: id.substring(0, i),
            name: id.substring(i + 1),
        };
    }

    docsUrl(path) {
        return docsUrl(path);
    }

    contextQuery() {
        const r = this.router.currentRoute;
        if (!r) {
            return {};
        }
        const { from, to, incident, alert } = r.query || {};
        return { from, to, incident, alert };
    }
}
