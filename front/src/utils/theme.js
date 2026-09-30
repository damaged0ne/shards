import Vue from 'vue';
import * as storage from '@/utils/storage';

const STORAGE_KEY = 'theme';
export const preferences = ['light', 'dark', 'auto'];

const media = window.matchMedia ? window.matchMedia('(prefers-color-scheme: dark)') : null;

// state is reactive, so components (e.g. the top bar toggle) can render the current theme.
export const state = Vue.observable({
    preference: readPreference(),
    dark: false,
});

let vuetify = null;

function readPreference() {
    try {
        const p = storage.local(STORAGE_KEY);
        return preferences.includes(p) ? p : 'auto';
    } catch {
        return 'auto';
    }
}

function resolveDark(preference) {
    if (preference === 'auto') {
        return !!(media && media.matches);
    }
    return preference === 'dark';
}

// apply syncs Vuetify's theme, the data-theme attribute used by the CSS tokens, and the legacy body class.
export function apply() {
    const dark = resolveDark(state.preference);
    state.dark = dark;
    if (vuetify) {
        vuetify.framework.theme.dark = dark;
    }
    document.documentElement.dataset.theme = dark ? 'dark' : 'light';
    document.body.classList.toggle('theme--dark', dark);
}

export function setPreference(preference) {
    state.preference = preferences.includes(preference) ? preference : 'auto';
    try {
        storage.local(STORAGE_KEY, state.preference);
    } catch {
        //
    }
    apply();
}

// toggle switches between light and dark explicitly (one-click toggle in the top bar).
export function toggle() {
    setPreference(state.dark ? 'light' : 'dark');
}

export function init(v) {
    vuetify = v;
    apply();
    if (media) {
        const onChange = () => state.preference === 'auto' && apply();
        media.addEventListener ? media.addEventListener('change', onChange) : media.addListener(onChange);
    }
}
