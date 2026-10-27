// The settings page (the kit frame with the Storage tab): `vite build --config settings/vite.config.mjs`
// writes settings/dist/, which scripts/build.sh hands to `ccu-addon-kit vendor --frame`.
import {fileURLToPath} from 'node:url';
import {defineConfig} from 'vite';
import {kitVite} from 'ccu-addon-kit/vite';

export default defineConfig({
    root: fileURLToPath(new URL('.', import.meta.url)),
    plugins: [kitVite()],
});
