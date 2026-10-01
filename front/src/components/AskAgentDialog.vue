<template>
    <v-dialog :value="value" @input="$emit('input', $event)" max-width="560">
        <v-card>
            <v-card-title class="d-flex align-center">
                <v-icon small class="mr-2">mdi-robot-outline</v-icon>
                Ask an agent
                <v-spacer />
                <v-btn icon small @click="$emit('input', false)"><v-icon small>mdi-close</v-icon></v-btn>
            </v-card-title>
            <v-card-text>
                <div class="caption grey--text mb-3">
                    Sends a signed task to the agent's dispatch webhook with a link to this {{ targetName }} and the MCP tool to call next. The
                    request is recorded in the timeline.
                </div>
                <v-progress-linear v-if="loading" indeterminate height="2" />
                <template v-else-if="agents.length">
                    <v-select
                        v-model="agentId"
                        :items="agents"
                        item-value="id"
                        item-text="name"
                        label="Agent"
                        outlined
                        dense
                        :item-disabled="(a) => !dispatchable(a)"
                    >
                        <template #item="{ item }">
                            <div class="d-flex align-center" style="gap: 8px; width: 100%">
                                <span class="mono">{{ item.name }}</span>
                                <Chip :tone="scopeClass[item.scope]">{{ item.scope }}</Chip>
                                <v-spacer />
                                <span v-if="!dispatchable(item)" class="caption grey--text">no webhook</span>
                            </div>
                        </template>
                    </v-select>
                    <v-textarea
                        v-model="instruction"
                        label="Instruction (optional)"
                        placeholder="e.g. Find out why checkout latency spiked after the 14:05 deploy; don't change anything, comment your findings."
                        outlined
                        dense
                        auto-grow
                        rows="3"
                        hide-details
                    />
                </template>
                <div v-else class="body-2">
                    No agents yet.
                    <router-link :to="{ name: 'overview', params: { view: 'agents', id: undefined }, query: $utils.contextQuery() }"
                        >Register one</router-link
                    >
                    and configure its dispatch webhook.
                </div>
                <v-alert v-if="error" color="error" outlined text dense class="mt-3 mb-0">{{ error }}</v-alert>
            </v-card-text>
            <v-card-actions>
                <v-spacer />
                <v-btn text @click="$emit('input', false)">Cancel</v-btn>
                <v-btn color="primary" :disabled="!agentId" :loading="sending" @click="send">Send</v-btn>
            </v-card-actions>
        </v-card>
    </v-dialog>
</template>

<script>
import { scopeClass } from '@/views/agents/agents';
import Chip from '@/views/agents/Chip.vue';

const targetNames = { incident: 'incident', alert: 'alert', alerting_rule: 'rule' };

export default {
    components: { Chip },
    props: {
        value: Boolean,
        targetType: String,
        targetId: String,
    },

    data() {
        return {
            agents: [],
            agentId: null,
            instruction: '',
            loading: false,
            sending: false,
            error: '',
        };
    },

    computed: {
        scopeClass() {
            return scopeClass;
        },
        targetName() {
            return targetNames[this.targetType] || 'object';
        },
    },

    watch: {
        value: {
            handler(v) {
                if (v) {
                    this.load();
                }
            },
            immediate: true,
        },
    },

    methods: {
        dispatchable(a) {
            return a.dispatch && a.dispatch.enabled && a.dispatch.url && a.status !== 'disabled' && a.status !== 'expired';
        },
        load() {
            this.error = '';
            this.loading = true;
            this.$api.getAgents((data, error) => {
                this.loading = false;
                if (error) {
                    this.error = error;
                    return;
                }
                this.agents = (data && data.agents) || [];
                const first = this.agents.find(this.dispatchable);
                if (!this.agentId || !this.agents.find((a) => a.id === this.agentId)) {
                    this.agentId = first ? first.id : null;
                }
            });
        },
        send() {
            this.sending = true;
            this.$api.agentAction(
                this.agentId,
                'ask',
                { target_type: this.targetType, target_id: this.targetId, instruction: this.instruction },
                (data, error) => {
                    this.sending = false;
                    if (error) {
                        this.error = error;
                        return;
                    }
                    this.instruction = '';
                    this.$emit('input', false);
                    this.$emit('sent', data);
                },
            );
        },
    },
};
</script>
