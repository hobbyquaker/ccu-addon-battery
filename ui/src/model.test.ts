import {describe, expect, it} from 'vitest';

import {compareDevices, formatBytes, formatVoltage, relativeTime, statusOf, type DeviceView} from './model.js';

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
