import {defineConfig, devices} from '@playwright/test';

// The browser checks run the built app (npm run build first) against the addon's real API, store
// and battery table, served by internal/e2eserver over a fake openccu-lite with a few devices. The
// specs share that one server and its state, so they run one after another. PORT lets two
// checkouts run side by side.
const PORT = Number(process.env.PORT || 8795);

export default defineConfig({
    testDir: 'e2e',
    timeout: 30_000,
    fullyParallel: false,
    workers: 1,
    retries: process.env.CI ? 1 : 0,
    reporter: 'list',
    use: {baseURL: `http://127.0.0.1:${PORT}/addons/battery/`, trace: 'retain-on-failure'},
    webServer: {command: `go run ../internal/e2eserver -addr 127.0.0.1:${PORT} -www dist`, url: `http://127.0.0.1:${PORT}/addons/battery/api/v1/devices`, reuseExistingServer: !process.env.CI, timeout: 120_000},
    projects: [
        {name: 'desktop-light', use: {...devices['Desktop Chrome'], colorScheme: 'light', locale: 'de-DE'}},
        {name: 'desktop-dark', use: {...devices['Desktop Chrome'], colorScheme: 'dark', locale: 'de-DE'}},
        {name: 'phone', use: {...devices['Pixel 7'], colorScheme: 'light', locale: 'de-DE'}},
    ],
});
