<template>
    <div>
        <div class="d-flex flex-wrap align-center mb-3" style="gap: 12px">
            <div class="caption" style="max-width: 760px">
                Operator agents (API keys and MCP clients) can be required to ask a human before risky actions. A pending action is stored with its
                arguments; when you approve it, it runs as the agent and the timeline records who approved it.
            </div>
            <v-spacer />
            <v-btn-toggle v-model="filter" mandatory dense @change="get">
                <v-btn value="pending" small>Pending</v-btn>
                <v-btn value="" small>All</v-btn>
            </v-btn-toggle>
        </div>

        <div class="layout">
            <div class="list">
                <div v-if="!loading && !approvals.length" class="empty">
                    {{ filter === 'pending' ? 'Nothing is waiting for approval.' : 'No agent actions have required approval yet.' }}
                </div>
                <ApprovalCard v-for="a in approvals" :key="a.id" :a="a" @decided="get" />
            </div>

            <div v-if="policy" class="policy">
                <div class="d-flex align-center">
                    <span class="font-weight-medium">Approval policy</span>
                    <v-spacer />
                    <v-btn v-if="policy.editable && dirty" x-small color="primary" depressed :loading="saving" @click="savePolicy">Save</v-btn>
                </div>
                <v-switch
                    v-model="policy.policy.require_approval"
                    label="Require human approval for agent actions"
                    dense
                    hide-details
                    inset
                    :disabled="!policy.editable"
                    class="mt-2"
                    @change="dirty = true"
                />
                <div class="caption mt-1 mb-2">When off, nothing waits for a human, but 'deny' still applies.</div>
                <div v-for="a in policy.actions" :key="a.action" class="policy-row">
                    <span class="flex-grow-1">{{ a.title }}</span>
                    <v-select
                        v-model="policy.policy.actions[a.action]"
                        :items="options"
                        dense
                        outlined
                        hide-details
                        :disabled="!policy.editable"
                        class="policy-select"
                        :menu-props="{ offsetY: true }"
                        @change="dirty = true"
                    />
                </div>
                <div v-if="!policy.editable" class="caption mt-2">Only project admins can change the policy.</div>
                <div v-if="policyError" class="error--text caption mt-2">{{ policyError }}</div>
            </div>
        </div>
    </div>
</template>

<script>
import ApprovalCard from '@/components/ApprovalCard.vue';

export default {
    components: { ApprovalCard },

    data() {
        return {
            approvals: [],
            filter: 'pending',
            loading: false,
            policy: null,
            dirty: false,
            saving: false,
            policyError: '',
        };
    },

    computed: {
        options() {
            return [
                { value: 'auto', text: 'auto' },
                { value: 'approval', text: 'approval' },
                { value: 'deny', text: 'deny' },
            ];
        },
    },

    mounted() {
        this.get();
        this.getPolicy();
        this.$events.watch(this, this.get, 'refresh');
    },

    methods: {
        get() {
            this.loading = true;
            this.$emit('loading', true);
            this.$api.getApprovals({ status: this.filter || undefined, limit: 200 }, (data, error) => {
                this.loading = false;
                this.$emit('loading', false);
                if (error) {
                    this.$emit('error', error);
                    return;
                }
                this.$emit('error', '');
                this.approvals = data || [];
            });
        },
        getPolicy() {
            this.$api.approvalPolicy(null, (data, error) => {
                if (error) {
                    this.policyError = error;
                    return;
                }
                this.policy = data;
                this.dirty = false;
            });
        },
        savePolicy() {
            this.saving = true;
            this.policyError = '';
            this.$api.approvalPolicy(this.policy.policy, (data, error) => {
                this.saving = false;
                if (error) {
                    this.policyError = error;
                    return;
                }
                this.policy = data;
                this.dirty = false;
            });
        },
    },
};
</script>

<style scoped>
.layout {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 340px;
    gap: 16px;
    align-items: start;
}
@media (max-width: 1000px) {
    .layout {
        grid-template-columns: minmax(0, 1fr);
    }
}
.list {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-width: 0;
}
.empty {
    border: 1px solid var(--border);
    border-radius: var(--r-lg);
    background: var(--surface);
    padding: 14px;
    font-size: 13px;
    color: var(--text-3);
}
.policy {
    border: 1px solid var(--border);
    border-radius: var(--r-lg);
    background: var(--surface);
    padding: 12px 14px;
}
.policy-row {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
    padding: 3px 0;
}
.policy-select {
    flex: none;
    width: 120px;
}
</style>
