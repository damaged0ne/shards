<template>
    <div>
        <p style="max-width: 800px">
            This integration enables shards to discover Azure Database for PostgreSQL and MySQL flexible servers and Azure Cache for Redis instances
            and collect their Azure Monitor metrics: CPU, memory, storage, IOPS, connections and replica lag. The cluster-agent authenticates with
            DefaultAzureCredential (a service principal, AKS workload identity or a managed identity), so nothing is configured here: declare the
            integration and the database credentials in the cluster-agent configuration file, see the
            <a :href="$utils.docsUrl('configuration/azure')" target="_blank">Azure integration</a> page.
        </p>
        <v-alert v-if="error" color="red" icon="mdi-alert-octagon-outline" outlined text class="mt-3" style="max-width: 800px">
            {{ error }}
        </v-alert>
        <CloudDiscovery provider="Azure" :configured="configured" :detected="detected" :errors="errors" :instances="instances" :error="error" />
    </div>
</template>

<script>
import CloudDiscovery from '../components/CloudDiscovery.vue';

export default {
    components: { CloudDiscovery },

    data() {
        return {
            error: '',
            configured: false,
            detected: false,
            errors: [],
            instances: [],
        };
    },

    mounted() {
        this.get();
    },

    methods: {
        get() {
            this.error = '';
            this.$api.getIntegrations('azure', (data, error) => {
                if (error) {
                    this.error = error;
                    return;
                }
                this.configured = !!data.view.configured;
                this.detected = !!data.view.detected;
                this.errors = data.view.errors || [];
                this.instances = data.view.instances || [];
            });
        },
    },
};
</script>
