import Vue from 'vue';
import Vuetify from 'vuetify/lib';

Vue.use(Vuetify);

// Keep in sync with the scales in src/styles/tokens.css (step 9/10 fills).
export default new Vuetify({
    icons: {
        iconfont: 'mdi',
    },
    theme: {
        themes: {
            light: {
                primary: '#6d4de6',
                secondary: '#1f9fcc',
                accent: '#7b5cf5',
                error: '#db2c2b',
                warning: '#ef9900',
                success: '#00a159',
                info: '#0073e1',
            },
            dark: {
                primary: '#8d72f8',
                secondary: '#39b8e0',
                accent: '#7b5cf5',
                error: '#ed413b',
                warning: '#ffa909',
                success: '#19b168',
                info: '#1b83f3',
            },
        },
    },
});
