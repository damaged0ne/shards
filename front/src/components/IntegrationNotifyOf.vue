<template>
    <div>
        <div class="subtitle-1">Notify of</div>
        <!-- eslint-disable-next-line vue/no-mutating-props -->
        <v-checkbox v-model="form.incidents" label="Incidents" dense hide-details />
        <v-checkbox :input-value="false" disabled label="Deployments (unavailable for this integration)" dense hide-details />
        <!-- eslint-disable-next-line vue/no-mutating-props -->
        <v-checkbox v-model="form.alerts" label="Alerts" dense hide-details />
        <!-- eslint-disable-next-line vue/no-mutating-props -->
        <v-checkbox v-model="form.comments" label="Comments on incidents and alerts (by people and agents)" dense hide-details />
    </div>
</template>

<script>
// shards fork: the "notify of" checkboxes of the Telegram, Discord, Mattermost and Email integrations.
export default {
    props: {
        form: Object,
    },
};
</script>
