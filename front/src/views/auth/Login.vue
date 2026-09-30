<template>
    <div class="form">
        <div class="brand">
            <img :src="`${$coroot.base_path}static/brand/icon.svg`" alt="" width="44" height="44" />
            <span class="wordmark">shards</span>
        </div>

        <v-card class="pa-6 pa-sm-8">
            <h2 class="text-h6 mb-1">
                <template v-if="set_admin_password">Set the admin password</template>
                <template v-else>Sign in</template>
            </h2>
            <div class="caption mb-6">Observability for your applications and infrastructure</div>

            <v-form v-model="valid" @submit.prevent="post" ref="form">
                <v-alert v-if="error" color="red" icon="mdi-alert-octagon-outline" outlined text>
                    {{ error }}
                </v-alert>
                <v-alert v-else-if="message" color="green" outlined text>
                    {{ message }}
                </v-alert>

                <div class="font-weight-medium">Email</div>
                <v-text-field
                    outlined
                    dense
                    type="email"
                    v-model="form.email"
                    name="email"
                    :rules="[$validators.notEmpty]"
                    :disabled="set_admin_password"
                />

                <div class="font-weight-medium">Password</div>
                <v-text-field outlined dense type="password" v-model="form.password" name="password" :rules="[$validators.notEmpty]" />

                <template v-if="set_admin_password">
                    <div class="font-weight-medium">Confirm password</div>
                    <v-text-field
                        outlined
                        dense
                        type="password"
                        v-model="confirm_password"
                        :rules="[$validators.notEmpty, (v) => v === form.password || 'passwords do not match']"
                    />
                </template>

                <v-btn block type="submit" :disabled="!valid" :loading="loading" color="primary" class="mt-5">
                    <template v-if="set_admin_password"> Set Admin Password and Log In </template>
                    <template v-else> Log In </template>
                </v-btn>
            </v-form>
        </v-card>

        <div v-if="!set_admin_password" class="caption text-center mt-6">Contact your shards administrator if you forgot your email or password.</div>
    </div>
</template>

<script>
export default {
    data() {
        return {
            form: {
                email: '',
                password: '',
            },
            confirm_password: '',
            valid: false,
            error: '',
            message: '',
            loading: false,
        };
    },

    computed: {
        set_admin_password() {
            return this.$route.query.action === 'set_admin_password';
        },
    },

    watch: {
        set_admin_password: {
            handler(v) {
                if (v) {
                    this.form.email = 'admin';
                }
            },
            immediate: true,
        },
    },

    methods: {
        post() {
            this.loading = true;
            this.error = '';
            const form = { ...this.form };
            if (this.set_admin_password) {
                form.action = 'set_admin_password';
            }
            this.$api.login(form, (data, error) => {
                this.loading = false;
                if (error) {
                    this.error = error;
                    return;
                }
                const next = this.$route.query.next;
                if (next && next.startsWith((this.$coroot.base_path || '/') + 'oauth/')) {
                    window.location.href = next;
                    return;
                }
                this.$router.push(next || { name: 'index' });
            });
        },
    },
};
</script>

<style scoped>
.form {
    max-width: 420px;
    margin: 12vh auto 48px;
    padding: 0 16px;
}
.brand {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 12px;
    margin-bottom: 28px;
}
.brand img {
    border-radius: 11px;
}
.wordmark {
    font-size: 28px;
    font-weight: 600;
    letter-spacing: 0.02em;
    color: var(--text-1);
}
</style>
