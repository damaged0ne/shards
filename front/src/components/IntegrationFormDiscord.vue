<template>
    <div>
        <div class="subtitle-1">To send notifications to a Discord channel:</div>
        <ol class="mb-4 caption">
            <li>
                Open <b>Server Settings</b> &rarr; <b>Integrations</b> &rarr; <b>Webhooks</b> (or the channel's <b>Edit Channel</b> &rarr;
                <b>Integrations</b>)
            </li>
            <li>Click <b>New Webhook</b>, choose the channel and, optionally, rename it (e.g. <i>shards</i>)</li>
            <li>Click <b>Copy Webhook URL</b> and paste it below</li>
        </ol>

        <div class="subtitle-1">Webhook URL</div>
        <v-text-field
            :value="form.webhook_url"
            @input="$set(form, 'webhook_url', $event)"
            outlined
            dense
            :rules="[$validators.notEmpty, $validators.isUrl]"
            placeholder="https://discord.com/api/webhooks/..."
        />

        <IntegrationNotifyOf :form="form" />
    </div>
</template>

<script>
import IntegrationNotifyOf from './IntegrationNotifyOf.vue';

export default {
    props: {
        form: Object,
    },
    components: { IntegrationNotifyOf },
};
</script>
