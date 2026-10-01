<template>
    <div class="mx-auto">
        <v-alert v-if="error" color="red" icon="mdi-alert-octagon-outline" outlined text>
            {{ error }}
        </v-alert>

        <h1 class="text-h5 mb-5">Configuration</h1>

        <v-tabs ref="tabs" :value="tab" height="40" show-arrows slider-size="2">
            <v-tab v-for="t in tabs" :key="t.id" :to="{ params: { tab: t.id } }" :disabled="t.disabled" :tab-value="t.id" exact>
                {{ t.name }}
            </v-tab>
        </v-tabs>

        <template v-if="!tab">
            <h2 class="text-h5 my-5">General project settings</h2>

            <v-form v-if="form" v-model="valid" ref="form" style="max-width: 800px">
                <v-alert v-if="readonly" color="primary" outlined text>
                    This project is defined through the config and cannot be modified via the UI.
                </v-alert>
                <v-alert v-if="multicluster" color="primary" outlined text>
                    This project aggregates telemetry from the member projects listed below.
                </v-alert>

                <v-form v-model="valid" :disabled="readonly" @submit.prevent="save">
                    <div class="subtitle-1">Project name</div>
                    <div class="caption">
                        Project is a separate cluster or environment, e.g. <var>production</var>, <var>staging</var> or <var>prod-us-west</var>.
                    </div>
                    <v-text-field v-model="form.name" :rules="[$validators.isSlug]" outlined dense required />

                    <div class="subtitle-1">Member projects</div>
                    <div class="caption">If defined, this project will serve as a multi-cluster representation of the configured projects.</div>

                    <v-autocomplete
                        :items="availableProjects"
                        v-model="form.member_projects"
                        color="primary"
                        multiple
                        outlined
                        dense
                        chips
                        small-chips
                        deletable-chips
                        hide-details
                        class="mb-6"
                        :disabled="readonly"
                    >
                        <template #selection="{ item }">
                            <v-chip
                                small
                                label
                                :close="!readonly"
                                close-icon="mdi-close"
                                @click:close="removeMemberProject(item)"
                                color="primary"
                                class="member"
                            >
                                <span :title="item">{{ item }}</span>
                            </v-chip>
                        </template>
                    </v-autocomplete>

                    <v-alert v-if="error" color="red" icon="mdi-alert-octagon-outline" outlined text>
                        {{ error }}
                    </v-alert>
                    <v-alert v-if="message" color="green" outlined text>
                        {{ message }}
                    </v-alert>
                    <v-btn block color="primary" @click="save" :disabled="readonly || !valid" :loading="loading">Save</v-btn>
                </v-form>
            </v-form>

            <template v-if="projectId">
                <template v-if="!multicluster">
                    <ProjectStatus :projectId="projectId" />
                    <ProjectApiKeys v-if="!multicluster" />
                </template>

                <h2 class="text-h5 mt-10 mb-5">Danger zone</h2>
                <ProjectDelete :projectId="projectId" />
            </template>
        </template>

        <template v-if="tab === 'prometheus'">
            <h1 class="text-h5 my-5">
                Prometheus integration
                <a :href="$utils.docsUrl('configuration/prometheus')" target="_blank">
                    <v-icon>mdi-information-outline</v-icon>
                </a>
            </h1>
            <IntegrationPrometheus />
        </template>

        <template v-if="tab === 'clickhouse'">
            <h1 class="text-h5 my-5">
                ClickHouse integration
                <a :href="$utils.docsUrl('configuration/clickhouse')" target="_blank">
                    <v-icon>mdi-information-outline</v-icon>
                </a>
            </h1>
            <p>
                shards stores
                <a :href="$utils.docsUrl('logs')" target="_blank">logs</a>, <a :href="$utils.docsUrl('tracing')" target="_blank">traces</a>, and
                <a :href="$utils.docsUrl('profiling')" target="_blank">profiles</a> in the ClickHouse database.
            </p>
            <IntegrationClickhouse />
        </template>

        <template v-if="tab === 'clouds' || tab === 'aws'">
            <h1 class="text-h5 my-5">Cloud integrations</h1>
            <p style="max-width: 800px">
                shards discovers the managed databases of your cloud provider and monitors them alongside your own services: RDS and ElastiCache on
                AWS, Cloud SQL and Memorystore on GCP, MySQL HeatWave, PostgreSQL and OCI Cache on Oracle Cloud, PostgreSQL and MySQL flexible servers
                and Azure Cache for Redis on Azure.
            </p>
            <h2 class="mt-8 mb-3">
                <img :src="`${$coroot.base_path}static/img/icons/aws${$vuetify.theme.dark ? '-dark' : ''}.svg`" height="28" alt="AWS" />
            </h2>
            <IntegrationAWS />
            <h2 class="mt-10 mb-3">
                <img :src="`${$coroot.base_path}static/img/icons/gcp${$vuetify.theme.dark ? '-dark' : ''}.svg`" height="28" alt="Google Cloud" />
            </h2>
            <IntegrationGCP />
            <h2 class="mt-10 mb-3"><img :src="`${$coroot.base_path}static/img/icons/oci.svg`" height="22" alt="Oracle Cloud" /></h2>
            <IntegrationOCI />
            <h2 class="mt-10 mb-3 d-flex align-center"><v-icon large color="#0078d4" class="mr-2">mdi-microsoft-azure</v-icon>Microsoft Azure</h2>
            <IntegrationAzure />
        </template>

        <template v-if="tab === 'applications'">
            <h2 class="text-h5 my-5" id="categories">
                Application categories
                <a :href="$utils.docsUrl('configuration/application-categories')" target="_blank">
                    <v-icon>mdi-information-outline</v-icon>
                </a>
            </h2>
            <p>
                You can organize your applications into groups by defining
                <a href="https://en.wikipedia.org/wiki/Glob_(programming)" target="_blank">glob patterns</a>
                in the <var>&lt;namespace&gt;/&lt;application_name&gt;</var> format. For Kubernetes applications, categories can also be defined by
                annotating Kubernetes objects. Refer the
                <a :href="$utils.docsUrl('configuration/application-categories')" target="_blank">documentation</a> for more details.
            </p>
            <ApplicationCategories />

            <h2 class="text-h5 mt-10 mb-5" id="service-map">
                Service map
                <a :href="$utils.docsUrl('configuration/service-map')" target="_blank">
                    <v-icon>mdi-information-outline</v-icon>
                </a>
            </h2>
            <ServiceMapSettings />

            <h2 class="text-h5 mt-10 mb-5" id="custom-applications">
                Custom applications
                <a :href="$utils.docsUrl('configuration/custom-applications')" target="_blank">
                    <v-icon>mdi-information-outline</v-icon>
                </a>
            </h2>

            <p>shards groups individual containers into applications using the following approach:</p>

            <ul class="mb-3">
                <li><b>Kubernetes metadata</b>: Pods are grouped into Deployments, StatefulSets, etc.</li>
                <li>
                    <b>Non-Kubernetes containers</b>: Containers such as Docker containers or Systemd units are grouped into applications by their
                    names. For example, Systemd services named <var>mysql</var> on different hosts are grouped into a single application called
                    <var>mysql</var>.
                </li>
            </ul>

            <p>
                This default approach works well in most cases. However, since no one knows your system better than you do, shards allows you to
                manually adjust application groupings to better fit your specific needs. You can match desired application instances by defining
                <a href="https://en.wikipedia.org/wiki/Glob_(programming)" target="_blank">glob patterns</a>
                for <var>instance_name</var>. Note that this does not apply to Kubernetes applications, which can be customized by annotating
                Kubernetes objects. Refer the <a :href="$utils.docsUrl('configuration/custom-applications')" target="_blank">documentation</a> for
                more details.
            </p>

            <CustomApplications />
        </template>

        <template v-if="tab === 'notifications'">
            <h1 class="text-h5 my-5">
                Notification integrations
                <a :href="$utils.docsUrl('alerting/slo-monitoring')" target="_blank">
                    <v-icon>mdi-information-outline</v-icon>
                </a>
            </h1>
            <Integrations />
        </template>

        <template v-if="tab === 'organization'">
            <h1 class="text-h5 my-5">
                Users
                <a :href="$utils.docsUrl('configuration/authentication')" target="_blank">
                    <v-icon>mdi-information-outline</v-icon>
                </a>
            </h1>
            <Users />
            <h1 class="text-h5 mt-10 mb-5">
                Role-Based Access Control (RBAC)
                <a :href="$utils.docsUrl('configuration/rbac')" target="_blank">
                    <v-icon>mdi-information-outline</v-icon>
                </a>
            </h1>
            <RBAC />
        </template>
    </div>
</template>

<script>
import ProjectApiKeys from './ProjectApiKeys.vue';
import ProjectDelete from './ProjectDelete.vue';
import ApplicationCategories from './ApplicationCategories.vue';
import Integrations from './Integrations.vue';
import IntegrationPrometheus from './IntegrationPrometheus.vue';
import IntegrationClickhouse from './IntegrationClickhouse.vue';
import IntegrationAWS from './IntegrationAWS.vue';
import IntegrationGCP from './IntegrationGCP.vue';
import IntegrationOCI from './IntegrationOCI.vue';
import IntegrationAzure from './IntegrationAzure.vue';
import CustomApplications from './CustomApplications.vue';
import ServiceMapSettings from './ServiceMapSettings.vue';
import Users from './Users.vue';
import RBAC from './RBAC.vue';
import ProjectStatus from '@/views/ProjectStatus.vue';

export default {
    props: {
        projectId: String,
        tab: String,
    },

    components: {
        ProjectStatus,
        CustomApplications,
        ServiceMapSettings,
        IntegrationPrometheus,
        IntegrationClickhouse,
        IntegrationAWS,
        IntegrationGCP,
        IntegrationOCI,
        IntegrationAzure,
        ProjectApiKeys,
        ProjectDelete,
        ApplicationCategories,
        Integrations,
        Users,
        RBAC,
    },

    data() {
        return {
            status: null,
            error: null,
            loading: false,
            form: {
                name: '',
                member_projects: [],
            },
            readonly: false,
            valid: false,
            message: '',
            availableProjects: [],
        };
    },

    watch: {
        projectId() {
            this.get();
        },
    },

    mounted() {
        // the slider is measured before the web font loads, when the fallback font makes the tabs wider
        document.fonts?.ready.then(() => this.$refs.tabs?.callSlider());
        this.get();
        if (!this.tabs.find((t) => t.id === this.tab)) {
            this.$router.replace({ params: { tab: undefined } });
        }
    },

    computed: {
        multicluster() {
            return this.form.member_projects !== undefined && this.form.member_projects.length > 0;
        },
        tabs() {
            const disabled = !this.projectId;
            return [
                { id: undefined, name: 'General' },
                { id: 'prometheus', name: 'Prometheus', disabled: disabled || this.multicluster },
                { id: 'clickhouse', name: 'Clickhouse', disabled: disabled || this.multicluster },
                { id: 'clouds', name: 'Cloud integrations', disabled },
                { id: 'applications', name: 'Applications', disabled },
                { id: 'notifications', name: 'Notifications', disabled },
                { id: 'organization', name: 'Organization' },
            ];
        },
    },

    methods: {
        get() {
            this.loading = true;
            this.error = '';
            this.$api.getProject(this.projectId, (data, error) => {
                this.loading = false;
                if (error) {
                    this.error = error;
                    return;
                }
                this.readonly = data.readonly;
                this.form.name = data.name;
                this.availableProjects = data.available_projects || [];
                this.form.member_projects = data.member_projects;
                if (!this.projectId && this.$refs.form) {
                    this.$refs.form.resetValidation();
                }
            });
        },
        save() {
            if (!this.valid) {
                return;
            }
            this.loading = true;
            this.error = '';
            this.message = '';
            this.$api.saveProject(this.projectId, this.form, (data, error) => {
                this.loading = false;
                if (error) {
                    this.error = error;
                    return;
                }
                this.$events.emit('projects');
                this.message = 'Settings were successfully updated.';
                setTimeout(() => {
                    this.message = '';
                }, 1000);
                if (!this.projectId) {
                    const projectId = data.trim();
                    this.$router.replace({ name: 'project_settings', params: { projectId, tab: 'prometheus' } }).catch((err) => err);
                }
            });
        },
        removeMemberProject(p) {
            const i = this.form.member_projects.indexOf(p);
            if (i >= 0) {
                this.form.member_projects.splice(i, 1);
            }
        },
    },
};
</script>

<style scoped>
*:deep(.v-list-item) {
    font-size: 14px !important;
    padding: 0 8px !important;
}
*:deep(.v-list-item__action) {
    margin: 4px !important;
}
.member {
    margin: 4px 4px 0 0 !important;
    padding: 0 8px !important;
}
.member span {
    max-width: 20ch;
    overflow: hidden;
    text-overflow: ellipsis;
}
.member:deep(.v-icon) {
    font-size: 16px !important;
}
</style>
