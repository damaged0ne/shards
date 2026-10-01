<template>
    <div class="connect">
        <div class="d-flex align-center mb-2">
            <v-icon small class="mr-2">mdi-connection</v-icon>
            <span class="font-weight-medium">Connect an agent</span>
            <v-spacer />
            <v-btn v-if="closable" icon x-small @click="$emit('close')"><v-icon x-small>mdi-close</v-icon></v-btn>
        </div>
        <ol class="steps">
            <li>Register the agent and create a key on its page (the key is scoped: read, triage, operator or admin).</li>
            <li>Point the agent's MCP client at the endpoint below with the key as a bearer token.</li>
            <li>Optionally configure a dispatch webhook so shards wakes the agent up on incidents, alerts and @mentions.</li>
        </ol>

        <div class="label">MCP endpoint (Streamable HTTP)</div>
        <div class="snippet">
            <code>{{ url }}</code>
            <CopyButton :text="url" />
        </div>

        <v-tabs v-model="tab" height="34" class="mt-3 tabs" show-arrows>
            <v-tab>Claude Code</v-tab>
            <v-tab>MCP client JSON</v-tab>
            <v-tab>curl</v-tab>
        </v-tabs>
        <div class="snippet block">
            <pre>{{ snippets[tab] }}</pre>
            <CopyButton :text="snippets[tab]" />
        </div>
        <div class="caption grey--text mt-2">
            Then ask the agent to call <span class="mono">list_projects</span>. Prompts <span class="mono">triage_incident</span>,
            <span class="mono">investigate_alert</span> and <span class="mono">write_postmortem</span> encode the recommended workflow. Interactive
            clients can also sign in with OAuth (unscoped, acts as the signed-in user).
        </div>
    </div>
</template>

<script>
import CopyButton from '@/components/CopyButton.vue';
import { mcpUrl } from './agents';

export default {
    components: { CopyButton },

    props: {
        apiKey: String,
        closable: { type: Boolean, default: true },
    },

    data() {
        return { tab: 0 };
    },

    computed: {
        url() {
            return mcpUrl(this.$coroot);
        },
        key() {
            return this.apiKey || '<agent-key>';
        },
        snippets() {
            const json = JSON.stringify(
                { mcpServers: { shards: { type: 'http', url: this.url, headers: { Authorization: `Bearer ${this.key}` } } } },
                null,
                2,
            );
            return [
                `claude mcp add --transport http shards ${this.url} --header "Authorization: Bearer ${this.key}"`,
                json,
                `curl -s ${this.url} -H "Authorization: Bearer ${this.key}" \\\n  -H "Content-Type: application/json" -H "Accept: application/json, text/event-stream" \\\n  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"1"}}}'`,
            ];
        },
    },
};
</script>

<style scoped>
.connect {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--r-md);
    padding: 14px 16px;
}
.steps {
    color: var(--text-2);
    font-size: 13px;
    margin-bottom: 12px;
}
.label {
    font-size: 11.5px;
    color: var(--text-2);
    margin-bottom: 4px;
}
.snippet {
    display: flex;
    align-items: center;
    gap: 8px;
    background: var(--surface-sunk);
    border: 1px solid var(--border);
    border-radius: var(--r-sm);
    padding: 4px 4px 4px 10px;
    min-width: 0;
}
.snippet code {
    background: none !important;
    padding: 0 !important;
    flex: 1;
    overflow-x: auto;
    white-space: nowrap;
    font-size: 12.5px;
}
.snippet.block {
    align-items: flex-start;
}
.snippet pre {
    flex: 1;
    margin: 0;
    padding: 6px 0;
    font-family: var(--mono);
    font-size: 12px;
    white-space: pre-wrap;
    word-break: break-all;
    min-width: 0;
}
.tabs:deep(.v-tabs-bar) {
    background: transparent !important;
}
.tabs:deep(.v-tab) {
    text-transform: none;
    font-size: 12.5px;
    letter-spacing: 0;
    min-width: 0;
    padding: 0 12px;
}
</style>
