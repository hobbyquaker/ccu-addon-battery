import 'occulite-ui/app.css';
import './app.css';
import {mount} from 'svelte';
import {addMessages, initI18n} from 'occulite-ui/i18n';
import {initTheme} from 'occulite-ui/theme';

import App from './App.svelte';
import {MESSAGES} from './messages.js';

addMessages(MESSAGES);
// inside openccu-lite's shell both follow the shell (theme= and lang= on the frame URL, the
// openccu-lite:theme message); opened on its own the page keeps its own choice
initI18n({storageKey: 'battery.language'});
initTheme({storageKey: 'battery.theme'});

mount(App, {target: document.getElementById('app')!});
