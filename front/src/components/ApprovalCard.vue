<template>
    <div class="approval" :class="{ pending: a.status === 'pending', compact }">
        <div class="head">
            <span class="bot"><v-icon size="15">mdi-robot-outline</v-icon></span>
            <div class="main">
                <div class="line">
                    <span class="who">{{ a.requested_by }}</span>
                    <span class="muted">{{ a.status === 'pending' ? 'is waiting for your approval to' : 'asked to' }}</span>
                    <span class="what">{{ actionName }}</span>
                </div>
                <div class="summary">{{ a.summary }}</div>
                <div v-if="a.reason" class="reason">“{{ a.reason }}”</div>
                <div class="meta">
                    <Chip :tone="statusChip">{{ a.status }}</Chip>
                    <span>#{{ a.id }}</span>
                    <span :title="$format.date(a.created_at, '{MMM} {DD}, {HH}:{mm}:{ss}')">{{ $format.timeSinceNow(a.created_at) }} ago</span>
                    <router-link v-if="showTarget && target" :to="target">{{ a.target_title || a.target_id }}</router-link>
                    <span v-if="a.decided_by">· {{ a.status === 'rejected' ? 'rejected' : 'approved' }} by {{ a.decided_by }}</span>
                    <span v-if="a.decision_comment" class="muted">“{{ a.decision_comment }}”</span>
                </div>
                <div v-if="a.status === 'failed' && a.result" class="error-text">{{ a.result }}</div>
            </div>
        </div>

        <div v-if="a.status === 'pending'" class="actions">
            <v-text-field
                v-if="commenting"
                v-model="comment"
                dense
                outlined
                hide-details
                placeholder="Comment (optional)"
                class="comment"
                @keydown.enter="decide('approve')"
            />
            <v-btn v-else x-small text class="add-comment" @click="commenting = true"><v-icon x-small left>mdi-comment-outline</v-icon>comment</v-btn>
            <v-spacer />
            <v-btn small outlined :loading="busy === 'reject'" :disabled="!!busy" @click="decide('reject')">Reject</v-btn>
            <v-btn small color="primary" depressed :loading="busy === 'approve'" :disabled="!!busy" @click="decide('approve')">Approve</v-btn>
        </div>
        <div v-if="error" class="error-text mt-1">{{ error }}</div>
    </div>
</template>

<script>
import Chip from '@/views/agents/Chip.vue';
import { agentActionName, approvalStatuses, targetRoute } from '@/utils/workflow';

export default {
    components: { Chip },

    props: {
        a: { type: Object, required: true },
        showTarget: { type: Boolean, default: true },
        compact: Boolean,
    },

    data() {
        return { busy: '', error: '', comment: '', commenting: false };
    },

    computed: {
        actionName() {
            return agentActionName(this.a.action).toLowerCase();
        },
        statusChip() {
            return approvalStatuses[this.a.status] || '';
        },
        target() {
            if (!this.a.target_type) {
                return this.a.action.includes('maintenance') ? targetRoute('maintenance_window', '', this.$utils.contextQuery()) : null;
            }
            return targetRoute(this.a.target_type, this.a.target_id, this.$utils.contextQuery());
        },
    },

    methods: {
        decide(decision) {
            this.busy = decision;
            this.error = '';
            this.$api.decideApproval(this.a.id, decision, this.comment.trim(), (data, error) => {
                this.busy = '';
                if (error) {
                    this.error = error;
                    return;
                }
                this.$emit('decided', data);
                this.$events.emit('refresh');
            });
        },
    },
};
</script>

<style scoped>
.approval {
    border: 1px solid var(--border);
    border-radius: var(--r-md);
    padding: 10px 12px;
    background: var(--surface);
}
.approval.pending {
    border-color: var(--warning-7);
    background: var(--warning-2);
}
.head {
    display: flex;
    gap: 10px;
    min-width: 0;
}
.bot {
    flex: none;
    width: 26px;
    height: 26px;
    border-radius: 50%;
    display: grid;
    place-items: center;
    background: var(--accent-4);
}
.bot .v-icon {
    color: var(--accent-11) !important;
}
.main {
    min-width: 0;
    flex: 1;
}
.line {
    font-size: 13.5px;
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
}
.who {
    font-weight: 600;
}
.what {
    font-weight: 500;
}
.muted {
    color: var(--text-2);
}
.summary {
    font-size: 13px;
    margin-top: 2px;
    word-break: break-word;
}
.reason {
    font-size: 13px;
    color: var(--text-2);
    margin-top: 2px;
}
.meta {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    margin-top: 6px;
    font-size: 12px;
    color: var(--text-2);
}
.actions {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 8px;
    padding-left: 36px;
}
.comment {
    max-width: 320px;
}
.add-comment {
    color: var(--text-2) !important;
    text-transform: none;
}
.error-text {
    color: var(--danger-11);
    font-size: 12.5px;
}
.compact .summary {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}
@media (max-width: 600px) {
    .actions {
        padding-left: 0;
        flex-wrap: wrap;
    }
}
</style>
