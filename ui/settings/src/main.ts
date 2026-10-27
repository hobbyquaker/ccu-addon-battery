// The settings page of the battery addon: the ccu-addon-kit frame (service, log, about - on a
// CCU; inside openccu-lite's shell the system has its own pages for those) with the addon's own
// tab, the database location. Built by Vite into settings/dist, which scripts/build.sh hands to
// `ccu-addon-kit vendor --frame`.
import 'occulite-ui/app.css';
import {defineTabs, mountFrame} from 'ccu-addon-kit/frame';

import strings from './i18n.js';
import Storage from './tabs/Storage.svelte';

const tabs = defineTabs([{id: 'storage', label: {de: 'Datenbank', en: 'Database'}, component: Storage}]);

void mountFrame(document.getElementById('app')!, {tabs, strings});
