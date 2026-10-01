<template>
    <div>
        <div class="subtitle-1">To send notifications to a Telegram chat:</div>
        <ol class="mb-4 caption">
            <li>Create a bot with <b>@BotFather</b> (<code>/newbot</code>) and copy its token</li>
            <li>Add the bot to a group or channel (in channels it must be an administrator)</li>
            <li>
                Find the chat id: post a message to the chat and open <code>https://api.telegram.org/bot&lt;token&gt;/getUpdates</code> (group ids
                start with <code>-100</code>), or use <code>@channelusername</code> for public channels
            </li>
        </ol>

        <div class="subtitle-1">Bot token</div>
        <!-- eslint-disable-next-line vue/no-mutating-props -->
        <v-text-field v-model="form.bot_token" outlined dense :rules="[$validators.notEmpty]" placeholder="123456789:AA..." />

        <div class="d-flex gap-3">
            <div class="flex-grow-1">
                <div class="subtitle-1">Chat id</div>
                <!-- eslint-disable-next-line vue/no-mutating-props -->
                <v-text-field v-model="form.chat_id" outlined dense :rules="[$validators.notEmpty]" placeholder="-1001234567890" />
            </div>
            <div class="flex-grow-1">
                <div class="subtitle-1">Topic (message thread id)</div>
                <v-text-field
                    :value="form.message_thread_id || ''"
                    @input="setThread"
                    outlined
                    dense
                    placeholder="optional, for forum groups"
                    :rules="[isThread]"
                />
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
    methods: {
        setThread(v) {
            this.$set(this.form, 'message_thread_id', v ? parseInt(v, 10) || 0 : 0);
        },
        isThread(v) {
            return !v || /^\d+$/.test(String(v)) || 'must be a number';
        },
    },
};
</script>
