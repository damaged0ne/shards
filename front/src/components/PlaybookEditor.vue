<template>
    <div class="playbook" :class="{ boxed: collapsible }">
        <div class="d-flex align-center head" :class="{ clickable: collapsible }" @click="collapsible && (open = !open)">
            <v-icon small class="mr-1">mdi-robot-outline</v-icon>
            <span class="font-weight-medium">Agent playbook</span>
            <span v-if="!loading && !playbook.body" class="caption grey--text ml-2">not set</span>
            <span v-else-if="playbook.updated_by" class="caption grey--text ml-2">
                updated by {{ playbook.updated_by }}, {{ $format.timeSinceNow(playbook.updated_at) }} ago
            </span>
            <v-spacer />
            <v-icon v-if="collapsible" small>{{ open ? 'mdi-chevron-up' : 'mdi-chevron-down' }}</v-icon>
        </div>
        <div v-if="open" class="mt-2">
            <div class="caption grey--text mb-2">
                Markdown read by operator agents (MCP <span class="mono">get_playbook</span>) before they act: what an agent may do, safe
                remediations, escalation contacts.
            </div>
            <template v-if="editing">
                <v-textarea v-model="body" outlined dense auto-grow rows="4" hide-details :placeholder="placeholder" class="body-2" />
                <div class="d-flex mt-2" style="gap: 8px">
                    <v-btn x-small color="primary" :loading="saving" @click="save">Save playbook</v-btn>
                    <v-btn x-small text @click="editing = false">Cancel</v-btn>
                </div>
            </template>
            <template v-else>
                <Markdown v-if="playbook.body" :src="playbook.body" :widgets="[]" class="body-2" />
                <v-btn v-if="editable" x-small outlined class="mt-1" @click="startEdit">{{ playbook.body ? 'Edit' : 'Write a playbook' }}</v-btn>
            </template>
            <v-alert v-if="error" color="error" outlined text dense class="mt-2 mb-0">{{ error }}</v-alert>
        </div>
    </div>
</template>

<script>
import Markdown from '@/components/Markdown.vue';

const placeholder = `## Allowed
- Restart a crash-looping pod once (kubectl rollout restart)
- Comment findings; resolve the alert after metrics are back to normal

## Not allowed
- Scaling the database, changing configs

## Escalation
- @oncall-db (Slack #db-oncall) for anything storage related`;

export default {
    components: { Markdown },

    props: {
        targetType: { type: String, required: true },
        targetId: { type: String, required: true },
        collapsible: Boolean,
    },

    data() {
        return {
            playbook: {},
            editable: false,
            loading: false,
            saving: false,
            editing: false,
            body: '',
            error: '',
            open: !this.collapsible,
        };
    },

    computed: {
        placeholder() {
            return placeholder;
        },
    },

    watch: {
        targetId() {
            this.load();
        },
    },

    mounted() {
        this.load();
    },

    methods: {
        load() {
            this.loading = true;
            this.$api.getPlaybook(this.targetType, this.targetId, (data, error) => {
                this.loading = false;
                if (error) {
                    this.error = error;
                    return;
                }
                this.error = '';
                this.playbook = data.playbook || {};
                this.editable = data.editable;
            });
        },
        startEdit() {
            this.body = this.playbook.body || '';
            this.editing = true;
        },
        save() {
            this.saving = true;
            this.$api.savePlaybook(this.targetType, this.targetId, this.body, (data, error) => {
                this.saving = false;
                if (error) {
                    this.error = error;
                    return;
                }
                this.error = '';
                this.editing = false;
                this.playbook = data.playbook || {};
            });
        },
    },
};
</script>

<style scoped>
.boxed {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--r-md);
    padding: 10px 14px;
}
.head {
    font-size: 13.5px;
}
.clickable {
    cursor: pointer;
}
</style>
