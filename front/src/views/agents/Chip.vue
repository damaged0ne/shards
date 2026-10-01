<template>
    <span class="agent-chip" :class="`tone-${tone || 'neutral'}`"><slot /></span>
</template>

<script>
// Like .status-chip, but with tone class names that don't collide with Vuetify's color helpers
// (.info / .success / .warning set a solid background with !important).
export default {
    props: {
        tone: String, // neutral | info | success | warning | danger
    },
};
</script>

<style scoped>
.agent-chip {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    height: 20px;
    padding: 0 7px;
    border-radius: 5px;
    font-size: 11.5px;
    font-weight: 500;
    line-height: 1;
    white-space: nowrap;
    border: 1px solid var(--gray-6);
    background: var(--gray-3);
    color: var(--gray-11);
}
.tone-info {
    background: var(--info-3);
    border-color: var(--info-6);
    color: var(--info-11);
}
.tone-success {
    background: var(--success-3);
    border-color: var(--success-6);
    color: var(--success-11);
}
.tone-warning {
    background: var(--warning-3);
    border-color: var(--warning-6);
    color: var(--warning-11);
}
.tone-danger {
    background: var(--danger-3);
    border-color: var(--danger-6);
    color: var(--danger-11);
}
.tone-accent {
    background: var(--accent-3);
    border-color: var(--accent-6);
    color: var(--accent-11);
}
</style>
