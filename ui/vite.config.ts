import {svelte} from '@sveltejs/vite-plugin-svelte';
import {defineConfig} from 'vitest/config';

// The app is served by the addon's Go backend at /addons/battery/ (and may sit at another base on
// a classic CCU), so every asset URL is relative.
export default defineConfig({
    base: './',
    plugins: [svelte()],
    resolve: {dedupe: ['svelte']},
    build: {
        outDir: 'dist',
        emptyOutDir: true,
        target: 'es2022',
        sourcemap: false,
    },
    server: {
        // development against a backend started with -no-session-check -port 8291
        proxy: {'/addons/battery/api': 'http://127.0.0.1:8291'},
    },
    test: {
        environment: 'jsdom',
        include: ['src/**/*.test.ts'],
    },
});
