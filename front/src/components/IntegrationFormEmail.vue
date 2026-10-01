<template>
    <div>
        <div class="subtitle-1 mb-2">Notifications are sent over SMTP as plain text + HTML emails.</div>

        <div class="d-flex gap-3">
            <div class="flex-grow-1">
                <div class="subtitle-1">SMTP host</div>
                <!-- eslint-disable-next-line vue/no-mutating-props -->
                <v-text-field v-model="form.host" outlined dense :rules="[$validators.notEmpty]" placeholder="smtp.example.com" />
            </div>
            <div style="width: 110px">
                <div class="subtitle-1">Port</div>
                <v-text-field :value="form.port" @input="setPort" outlined dense placeholder="587" />
            </div>
            <div style="width: 170px">
                <div class="subtitle-1">Encryption</div>
                <!-- eslint-disable-next-line vue/no-mutating-props -->
                <v-select v-model="form.tls_mode" :items="tlsModes" outlined dense :menu-props="{ offsetY: true }" />
            </div>
        </div>

        <div class="d-flex gap-3">
            <div class="flex-grow-1">
                <div class="subtitle-1">Username</div>
                <!-- eslint-disable-next-line vue/no-mutating-props -->
                <v-text-field v-model="form.username" outlined dense placeholder="optional" autocomplete="off" />
            </div>
            <div class="flex-grow-1">
                <div class="subtitle-1">Password</div>
                <!-- eslint-disable-next-line vue/no-mutating-props -->
                <v-text-field v-model="form.password" outlined dense type="password" placeholder="optional" autocomplete="new-password" />
            </div>
        </div>
        <!-- eslint-disable-next-line vue/no-mutating-props -->
        <v-checkbox v-model="form.tls_skip_verify" label="Skip TLS certificate verification" dense hide-details class="mt-0 mb-3" />

        <div class="subtitle-1">From</div>
        <!-- eslint-disable-next-line vue/no-mutating-props -->
        <v-text-field v-model="form.from" outlined dense :rules="[$validators.notEmpty]" placeholder="shards <alerts@example.com>" />

        <div class="subtitle-1">To</div>
        <v-text-field
            :value="toText"
            @input="setTo"
            outlined
            dense
            :rules="[$validators.notEmpty]"
            placeholder="ops@example.com, oncall@example.com"
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
    data() {
        return {
            toText: (this.form.to || []).join(', '),
            tlsModes: [
                { value: 'starttls', text: 'STARTTLS' },
                { value: 'tls', text: 'TLS (SMTPS)' },
                { value: 'none', text: 'None' },
            ],
        };
    },
    watch: {
        form(f) {
            this.toText = (f.to || []).join(', ');
        },
    },
    methods: {
        setTo(v) {
            this.toText = v;
            this.$set(
                this.form,
                'to',
                (v || '')
                    .split(',')
                    .map((s) => s.trim())
                    .filter(Boolean),
            );
        },
        setPort(v) {
            this.$set(this.form, 'port', parseInt(v, 10) || 0);
        },
    },
};
</script>
