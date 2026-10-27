// The settings page's strings: the storage texts the app also uses (one source, src/messages.ts),
// as the kit frame takes them - {key: {de, en}}.
import {MESSAGES} from '../../src/messages.js';

const strings: Record<string, {de: string; en: string}> = {
    cancel: {de: 'Abbrechen', en: 'Cancel'},
};
for (const [key, value] of Object.entries(MESSAGES)) {
    if ((key.startsWith('storage.') || key.startsWith('notify.') || key === 'time.never') && typeof value.de === 'string' && typeof value.en === 'string') {
        strings[key] = {de: value.de, en: value.en};
    }
}

export default strings;
