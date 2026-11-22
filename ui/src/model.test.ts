import {describe, expect, it} from 'vitest';

import {DEFAULT_DEVICE_FILTER, baselineOf, batteryName, chemistryLabel, originsOf, overrideOf, profileForm, profileFormValid, checkState, compareDevices, describeSlots, evidenceSummary, filterDevices, formatBytes, formatCents, formatDays, formatRange, formatVoltage, fromLocalInput, isMains, isStick, lifeText, loadDeviceFilter, locationText, modelInitials, parsePrice, priceInput, relativeTime, saveDeviceFilter, statusOf, toLocalInput, type DeviceView} from './model.js';

const dev = (p: Partial<DeviceView>): DeviceView => ({
    interface: 'HmIP-RF',
    address: 'X',
    name: '',
    rooms: [],
    model: 'HmIP-WRC2',
    power: 'unknown',
    voltage: null,
    low_bat: null,
    unreach: null,
    first_seen: 0,
    ...p,
});

describe('statusOf', () => {
    it('ranks unreachable over low over external over ok', () => {
        expect(statusOf(dev({unreach: true, low_bat: true}))).toBe('unreach');
        expect(statusOf(dev({low_bat: true, voltage_status: 'EXTERNAL'}))).toBe('low');
        expect(statusOf(dev({low_bat: false, voltage_status: 'EXTERNAL'}))).toBe('external');
        expect(statusOf(dev({power: 'mains', voltage: 230}))).toBe('external');
        expect(statusOf(dev({low_bat: false}))).toBe('ok');
        expect(statusOf(dev({voltage: 2.9}))).toBe('ok');
        expect(statusOf(dev({}))).toBe('unknown');
    });
    it('says ignored first for a device the user does not monitor', () => {
        expect(statusOf(dev({ignored: true, low_bat: true, unreach: true}))).toBe('ignored');
        expect(statusOf(dev({ignored: false, low_bat: true}))).toBe('low');
    });
});

describe('formatting', () => {
    it('formats volts in the language', () => {
        expect(formatVoltage(2.9, 'en')).toBe('2.90 V');
        expect(formatVoltage(2.9, 'de')).toBe('2,90 V');
        expect(formatVoltage(null)).toBe('');
    });
    it('formats relative times and falls back to the date', () => {
        const now = Date.UTC(2026, 9, 27, 12, 0, 0);
        expect(relativeTime(now - 30_000, now, 'en')).toBe('30 seconds ago');
        expect(relativeTime(now - 3 * 3_600_000, now, 'en')).toBe('3 hours ago');
        expect(relativeTime(now - 2 * 86_400_000, now, 'en')).toBe('2 days ago');
        expect(relativeTime(now - 30 * 86_400_000, now, 'en')).toMatch(/2026/);
        expect(relativeTime(undefined, now)).toBe('');
    });
    it('formats bytes', () => {
        expect(formatBytes(512)).toBe('512 B');
        expect(formatBytes(20 * 1024)).toBe('20 kB');
        expect(formatBytes(12.3 * 1024 * 1024)).toBe('12.3 MB');
    });
});

describe('compareDevices', () => {
    it('puts the worst first, then by name', () => {
        const list = [
            dev({name: 'b', low_bat: false}),
            dev({name: 'a', low_bat: false}),
            dev({name: 'c', unreach: true}),
            dev({name: 'd', low_bat: true}),
            dev({name: 'e', voltage_status: 'EXTERNAL', low_bat: false}),
        ].sort(compareDevices);
        expect(list.map((d) => d.name)).toEqual(['c', 'd', 'a', 'b', 'e']);
    });
});

describe('the P1 helpers', () => {
    it('describes slots', () => {
        expect(describeSlots([{count: 2, type: 'AAA'}])).toBe('2× AAA');
        expect(describeSlots([{count: 1, type: 'CR2032'}, {count: 2, type: 'AA'}])).toBe('1× CR2032 + 2× AA');
        expect(describeSlots([])).toBe('');
        expect(describeSlots(undefined)).toBe('');
    });
    it('separates mains from battery devices', () => {
        for (const p of ['mains', 'bus', 'host', 'external']) expect(isMains(p)).toBe(true);
        for (const p of ['battery', 'rechargeable', 'battery-or-dc', 'unknown', undefined]) expect(isMains(p)).toBe(false);
        expect(statusOf(dev({power: 'bus', low_bat: false}))).toBe('external');
    });
    it('tells the voltage story of a detection', () => {
        expect(evidenceSummary({triggers: ['A', 'B'], voltage_before: 2.8, voltage_after: 3.1, voltage_confirmed: 3.08, window_s: 86400, last_trigger: 0}, 'en')).toBe('2.80 V → 3.10 V, 3.08 V after 24 h');
        expect(evidenceSummary({triggers: ['C'], last_trigger: 0}, 'en')).toBe('');
        expect(evidenceSummary(undefined)).toBe('');
    });
    it('round-trips a datetime-local value', () => {
        const ms = new Date(2026, 9, 27, 11, 26).getTime();
        expect(fromLocalInput(toLocalInput(ms))).toBe(ms);
        expect(Number.isNaN(fromLocalInput(''))).toBe(true);
    });
});

describe('the catalogue', () => {
    it('has every key the pages use, in German and English', async () => {
        const {MESSAGES} = await import('./messages.js');
        const files = import.meta.glob('./*.svelte', {query: '?raw', import: 'default', eager: true}) as Record<string, string>;
        const keys = new Set<string>();
        for (const src of Object.values(files)) {
            for (const m of src.matchAll(/\bt\('([^']+)'/g)) keys.add(m[1] ?? '');
        }
        // the keys built at run time
        for (const p of ['status.ok', 'status.low', 'status.unreach', 'status.external', 'status.unknown']) keys.add(p);
        for (const k of ['detected', 'confirmed', 'manual', 'import']) keys.add('kind.' + k);
        for (const r of ['A', 'B', 'C', 'D']) keys.add('rule.' + r);
        for (const c of ['high', 'medium', 'low']) keys.add('confidence.' + c);
        for (const s of ['table', 'override', 'guess', 'none']) keys.add('profile.' + s);
        for (const s of ['created', 'merged', 'duplicate']) keys.add('change.record.' + s);
        expect(keys.size).toBeGreaterThan(60);
        const missing = [...keys].filter((k) => !k.endsWith('.') && !(k in MESSAGES));
        expect(missing).toEqual([]);
        for (const [k, v] of Object.entries(MESSAGES)) {
            expect(v, k).toHaveProperty('de');
            expect(v, k).toHaveProperty('en');
        }
    });
});

describe('durations and prices', () => {
    it('formats days as people say them', () => {
        expect(formatDays(12, 'de')).toBe('12 T.');
        expect(formatDays(12, 'en')).toBe('12 d');
        expect(formatDays(45, 'de')).toBe('1 Mon.');
        expect(formatDays(243, 'en')).toBe('8 mo');
        expect(formatDays(365, 'de')).toBe('1 J.');
        expect(formatDays(456, 'de')).toBe('1 J. 3 Mon.');
        expect(formatDays(undefined)).toBe('');
    });
    it('builds the life column', () => {
        expect(lifeText({count: 0})).toBe('');
        expect(lifeText({count: 1, avg_days: 400, min_days: 400, max_days: 400}, 'de')).toBe('1 J. 1 Mon. (1×)');
        expect(lifeText({count: 3, avg_days: 426, min_days: 274, max_days: 578}, 'de')).toBe('Ø 1 J. 2 Mon. (9 Mon. – 1 J. 7 Mon., 3×)');
        expect(lifeText({count: 3, avg_days: 300, min_days: 274, max_days: 335}, 'de')).toBe('Ø 10 Mon. (9–11 Mon., 3×)');
        expect(formatRange(12, 30, 'en')).toBe('12–30 d');
        expect(formatRange(40, 40, 'de')).toBe('1 Mon.');
    });
    it('parses and formats prices', () => {
        expect(parsePrice('0,89')).toBe(89);
        expect(parsePrice('0.89')).toBe(89);
        expect(parsePrice('1')).toBe(100);
        expect(parsePrice('1,5 €')).toBe(150);
        expect(parsePrice('')).toBeUndefined();
        expect(parsePrice('abc')).toBeNaN();
        expect(parsePrice('1,234')).toBeNaN();
        expect(priceInput(89, 'de')).toBe('0,89');
        expect(priceInput(89, 'en')).toBe('0.89');
        expect(priceInput(undefined)).toBe('');
        expect(formatCents(89, 'de')).toMatch(/0,89/);
        expect(formatCents(1250, 'en')).toMatch(/12\.50/);
    });
    it('names batteries and placeholders', () => {
        expect(batteryName({vendor: 'Varta', product: 'Power on Demand', type: 'AA'})).toBe('Varta · Power on Demand · AA');
        expect(batteryName({type: 'AAA'}, 'ohne Angabe')).toBe('ohne Angabe · AAA');
        expect(modelInitials('HmIP-WRC2')).toBe('WR');
        expect(modelInitials('HM-Sec-SC-2')).toBe('SE');
    });
});

describe('checkState', () => {
    it('answers the end of a candidate\'s check and whether it has passed', () => {
        expect(checkState({status: 'candidate', check_until: 2000}, 1000)).toEqual({until: 2000, late: false});
        expect(checkState({status: 'candidate', check_until: 2000}, 2000)).toEqual({until: 2000, late: true});
        expect(checkState({status: 'candidate'}, 1000)).toBeUndefined();
        expect(checkState({status: 'pending', check_until: 2000}, 1000)).toBeUndefined();
        expect(checkState({status: 'accepted'}, 1000)).toBeUndefined();
    });
});

describe('the device list\'s filter', () => {
    const list = [
        dev({address: 'OK', low_bat: false}),
        dev({address: 'GONE', unreach: true}),
        dev({address: 'MUTED', low_bat: false, service_messages_off: true}),
        dev({address: 'BOTH', unreach: true, service_messages_off: true}),
        dev({address: 'MAINS', power: 'mains', unreach: true}),
    ];
    const ids = (l: DeviceView[]) => l.map((d) => d.address).join(',');
    const ccu = {service_messages: true};
    const lite = {service_messages: false};

    it('shows what the list always showed by default', () => {
        expect(ids(filterDevices(list, DEFAULT_DEVICE_FILTER, ccu))).toBe('OK,GONE,MUTED,BOTH');
        expect(ids(filterDevices(list, {...DEFAULT_DEVICE_FILTER, showMains: true}, ccu))).toBe('OK,GONE,MUTED,BOTH,MAINS');
    });
    it('hides unreachable devices, and devices with service messages off', () => {
        expect(ids(filterDevices(list, {...DEFAULT_DEVICE_FILTER, hideUnreach: true}, ccu))).toBe('OK,MUTED');
        expect(ids(filterDevices(list, {...DEFAULT_DEVICE_FILTER, hideServiceOff: true}, ccu))).toBe('OK,GONE');
        expect(ids(filterDevices(list, {showMains: true, hideUnreach: true, hideServiceOff: true, showIgnored: false}, ccu))).toBe('OK');
    });
    it('hides the ignored devices unless asked for (1.5.0)', () => {
        const withIgnored = [...list, dev({address: 'IGN', low_bat: true, ignored: true}), dev({address: 'IGNM', power: 'mains', ignored: true})];
        expect(ids(filterDevices(withIgnored, DEFAULT_DEVICE_FILTER, ccu))).toBe('OK,GONE,MUTED,BOTH');
        expect(ids(filterDevices(withIgnored, {...DEFAULT_DEVICE_FILTER, showIgnored: true}, ccu))).toBe('OK,GONE,MUTED,BOTH,IGN');
        expect(ids(filterDevices(withIgnored, {...DEFAULT_DEVICE_FILTER, showIgnored: true, showMains: true}, ccu))).toBe('OK,GONE,MUTED,BOTH,MAINS,IGN,IGNM');
    });
    it('hides nothing by a switch the system does not have', () => {
        expect(ids(filterDevices(list, {...DEFAULT_DEVICE_FILTER, hideServiceOff: true}, lite))).toBe('OK,GONE,MUTED,BOTH');
    });
    it('remembers the choice, and falls back to the default', () => {
        const mem = new Map<string, string>();
        const storage = {getItem: (k: string) => mem.get(k) ?? null, setItem: (k: string, v: string) => void mem.set(k, v)};
        expect(loadDeviceFilter(storage)).toEqual(DEFAULT_DEVICE_FILTER);
        saveDeviceFilter({showMains: false, hideUnreach: true, hideServiceOff: true, showIgnored: true}, storage);
        expect(loadDeviceFilter(storage)).toEqual({showMains: false, hideUnreach: true, hideServiceOff: true, showIgnored: true});
        // a choice saved by 1.4 (no showIgnored) hides the ignored ones
        mem.set('battery.devices.filter', '{"showMains":true}');
        expect(loadDeviceFilter(storage)).toEqual({...DEFAULT_DEVICE_FILTER, showMains: true});
        mem.set('battery.devices.filter', '{broken');
        expect(loadDeviceFilter(storage)).toEqual(DEFAULT_DEVICE_FILTER);
        mem.set('battery.devices.filter', '{"hideUnreach":"yes","x":1}');
        expect(loadDeviceFilter(storage)).toEqual(DEFAULT_DEVICE_FILTER);
        expect(loadDeviceFilter(undefined)).toEqual(DEFAULT_DEVICE_FILTER);
        const refusing = {
            getItem: () => {
                throw new Error('denied');
            },
            setItem: () => {
                throw new Error('denied');
            },
        };
        expect(loadDeviceFilter(refusing)).toEqual(DEFAULT_DEVICE_FILTER);
        expect(() => saveDeviceFilter(DEFAULT_DEVICE_FILTER, refusing)).not.toThrow();
    });
});

describe('storage locations', () => {
    it('names a stick by its label, its UUID or its mount point', () => {
        expect(locationText('userfs')).toEqual({key: 'storage.candidate.userfs', params: {}});
        expect(locationText('usb:MY STICK')).toEqual({key: 'storage.candidate.usb', params: {label: 'MY STICK'}});
        expect(locationText('usb-uuid:1A2B-3C4D')).toEqual({key: 'storage.candidate.usbUuid', params: {uuid: '1A2B-3C4D'}});
        expect(locationText('usb-path:/media/usb1')).toEqual({key: 'storage.candidate.usbPath', params: {path: '/media/usb1'}});
    });
    it('knows a stick from the user partition', () => {
        expect(['usb:X', 'usb-uuid:1A2B-3C4D', 'usb-path:/media/usb1'].map(isStick)).toEqual([true, true, true]);
        expect(isStick('userfs')).toBe(false);
    });
});

describe('the battery editor', () => {
    const wrc2 = baselineOf({power: 'battery', slots: [{count: 2, type: 'AAA'}]});
    const none = baselineOf(null);

    it('fills the fields from the override over its baseline', () => {
        expect(profileForm(wrc2, null)).toEqual({power: 'battery', slots: [{count: 2, type: 'AAA'}], chemistry: '', rechargeable: false, outdoor: false});
        expect(profileForm(wrc2, {slots: [{count: 2, type: 'AA'}], outdoor: true, rechargeable: true})).toEqual({power: 'battery', slots: [{count: 2, type: 'AA'}], chemistry: '', rechargeable: true, outdoor: true});
    });

    it('a model the table lacks: prefilled with what the device shows (a guess), unknown otherwise', () => {
        expect(profileForm(none, null, {source: 'guess', power: 'battery', slots: [{count: 2, type: 'AA'}]})).toMatchObject({power: 'battery', slots: [{count: 2, type: 'AA'}]});
        expect(profileForm(none, null, {source: 'none', power: 'unknown', slots: []})).toMatchObject({power: 'unknown', slots: []});
        expect(profileForm(none, null, {source: 'datapoints', power: 'mains', slots: []}).power).toBe('mains');
    });

    it('writes only what differs from the baseline; nothing differs: no override', () => {
        expect(overrideOf(profileForm(wrc2, null), wrc2)).toBeNull();
        expect(overrideOf({...profileForm(wrc2, null), outdoor: true}, wrc2)).toEqual({outdoor: true});
        expect(overrideOf({...profileForm(wrc2, null), slots: [{count: 2, type: 'AA'}]}, wrc2)).toEqual({slots: [{count: 2, type: 'AA'}]});
        expect(overrideOf({...profileForm(wrc2, null), rechargeable: true, chemistry: 'lithium-aa'}, wrc2)).toEqual({chemistry: 'lithium-aa', rechargeable: true});
        expect(overrideOf({power: 'battery', slots: [{count: 1, type: 'CR2032'}], chemistry: '', rechargeable: false, outdoor: false}, none)).toEqual({power: 'battery', slots: [{count: 1, type: 'CR2032'}]});
    });

    it('a power without batteries drops the slots and the chemistry', () => {
        expect(overrideOf({power: 'external', slots: [{count: 3, type: 'C'}], chemistry: 'nimh', rechargeable: true, outdoor: false}, none)).toEqual({power: 'external'});
        expect(overrideOf({power: 'mains', slots: [{count: 1, type: 'AA'}], chemistry: '', rechargeable: false, outdoor: false}, wrc2)).toEqual({power: 'mains'});
        expect(profileFormValid({power: 'mains', slots: [{count: 0, type: 'X'}], chemistry: '', rechargeable: false, outdoor: false}, ['AA'])).toBe(true);
    });

    it('refuses what the API refuses: a count outside 1-12, an unknown type', () => {
        const f = (count: number, type: string) => profileFormValid({power: 'battery', slots: [{count, type}], chemistry: '', rechargeable: false, outdoor: false}, ['AA', 'AAA']);
        expect(f(2, 'AA')).toBe(true);
        expect(f(0, 'AA')).toBe(false);
        expect(f(13, 'AA')).toBe(false);
        expect(f(1.5, 'AA')).toBe(false);
        expect(f(2, 'AAAA')).toBe(false);
    });

    it('says per value where it comes from: this device over the model over the table', () => {
        const p = {source: 'override' as const, power: 'battery', slots: [{count: 2, type: 'AA'}]};
        expect(originsOf(p, null, null, true)).toEqual({power: 'table', slots: 'table', chemistry: 'table', outdoor: 'table'});
        expect(originsOf(p, null, {slots: [{count: 2, type: 'AA'}], outdoor: true}, true)).toEqual({power: 'table', slots: 'model', chemistry: 'table', outdoor: 'model'});
        expect(originsOf(p, {rechargeable: true, slots: [{count: 1, type: 'AA'}]}, {slots: [{count: 2, type: 'AA'}]}, true)).toEqual({power: 'table', slots: 'device', chemistry: 'device', outdoor: 'table'});
        expect(originsOf({source: 'guess', power: 'battery', slots: []}, null, null, false)).toEqual({power: 'auto', slots: 'auto', chemistry: 'none', outdoor: 'none'});
        expect(originsOf({source: 'table', power: 'external', auto: true, slots: []}, null, null, true).power).toBe('auto');
        expect(originsOf({source: 'override', power: 'battery', slots: []}, null, {power: 'battery'}, false).power).toBe('model');
    });

    it('names the chemistries, an unknown one as it is', () => {
        const tr = (k: string) => `<${k}>`;
        expect(chemistryLabel('nimh', tr)).toBe('<chemistry.nimh>');
        expect(chemistryLabel('zinc-air', tr)).toBe('zinc-air');
    });
});
