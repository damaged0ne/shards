<template>
    <div>
        <v-alert v-if="error" color="red" icon="mdi-alert-octagon-outline" outlined text class="mt-2">
            {{ error }}
        </v-alert>
        <p>
            Access is controlled by three built-in roles: Admin, Editor, and Viewer. The table below lists the actions each role is allowed to
            perform.
        </p>
        <v-simple-table v-if="!error" dense class="table mt-5">
            <thead>
                <tr>
                    <th>Action</th>
                    <th v-for="r in roles">{{ r.name }}</th>
                </tr>
            </thead>
            <tbody>
                <tr v-for="a in actions">
                    <td>{{ a.name }}</td>
                    <td v-for="r in a.roles">
                        <v-icon v-if="!r.objects" small color="error">mdi-close-thick</v-icon>
                        <v-icon v-else-if="!r.objects.length" small color="success">mdi-check-bold</v-icon>
                        <v-tooltip v-else bottom>
                            <template #activator="{ on }">
                                <v-icon v-on="on" small color="success">mdi-list-status</v-icon>
                            </template>
                            <v-card class="pa-2">
                                <div v-for="o in r.objects">{{ o }}</div>
                            </v-card>
                        </v-tooltip>
                    </td>
                </tr>
            </tbody>
        </v-simple-table>
    </div>
</template>

<script>
export default {
    data() {
        return {
            loading: false,
            error: '',
            roles: [],
            actions: [],
        };
    },

    mounted() {
        this.get();
    },

    methods: {
        get() {
            this.loading = true;
            this.error = '';
            this.$api.roles(null, (data, error) => {
                this.loading = false;
                if (error) {
                    this.error = error;
                    return;
                }
                this.roles = (data.roles || []).filter((r) => !r.custom);
                const builtin = new Set(this.roles.map((r) => r.name));
                this.actions = (data.actions || []).map((a) => ({ ...a, roles: (a.roles || []).filter((r) => builtin.has(r.name)) }));
            });
        },
    },
};
</script>

<style scoped>
.table:deep(th),
.table:deep(td) {
    padding: 0 8px !important;
}
</style>
