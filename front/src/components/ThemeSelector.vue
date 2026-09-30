<template>
    <v-list-item-group :value="theme.preference" mandatory>
        <v-list-item v-for="(t, name) in themes" :key="name" :value="name" @click="set(name)">
            <v-icon small class="mr-2">{{ t.icon }}</v-icon>
            {{ t.title }}
        </v-list-item>
    </v-list-item-group>
</template>

<script>
import { state, setPreference } from '@/utils/theme';

export default {
    computed: {
        theme() {
            return state;
        },
        themes() {
            return {
                light: { icon: 'mdi-weather-sunny', title: 'Light' },
                dark: { icon: 'mdi-weather-night', title: 'Dark' },
                auto: { icon: 'mdi-theme-light-dark', title: 'System' },
            };
        },
    },

    methods: {
        set(name) {
            setPreference(name);
        },
    },
};
</script>
