import {describe, expect, it} from 'vitest';

import {batteryName, compareDevices, describeSlots, evidenceSummary, formatBytes, formatCents, formatDays, formatVoltage, fromLocalInput, isMains, lifeText, modelInitials, parsePrice, priceInput, relativeTime, statusOf, toLocalInput, type DeviceView} from './model.js';

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
