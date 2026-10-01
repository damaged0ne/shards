<template>
    <div>
        <div class="subtitle-1">To send notifications to Mattermost:</div>
        <ol class="mb-4 caption">
            <li>Go to <b>Integrations</b> &rarr; <b>Incoming Webhooks</b> &rarr; <b>Add Incoming Webhook</b></li>
            <li>Choose the default channel (and, optionally, allow overriding it)</li>
            <li>Copy the webhook URL and paste it below</li>
        </ol>

        <div class="subtitle-1">Webhook URL</div>
        <v-text-field
            :value="form.webhook_url"
            @input="$set(form, 'webhook_url', $event)"
            outlined
            dense
            :rules="[$validators.notEmpty, $validators.isUrl]"
            placeholder="https://mattermost.example.com/hooks/..."
        />

        <div class="d-flex gap-3">
            <div class="flex-grow-1">
                <div class="subtitle-1">Channel</div>
                <!-- eslint-disable-next-line vue/no-mutating-props -->
                <v-text-field v-model="form.channel" outlined dense placeholder="the webhook's default channel" />
            </div>
            <div class="flex-grow-1">
                <div class="subtitle-1">Username</div>
                <!-- eslint-disable-next-line vue/no-mutating-props -->
                <v-text-field v-model="form.username" outlined dense placeholder="the webhook's default username" />
            </div>
        </div>

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
