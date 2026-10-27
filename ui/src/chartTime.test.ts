import {describe, expect, it, vi} from 'vitest';

// uPlot asks matchMedia for the pixel ratio when it loads; jsdom has none
vi.hoisted(() => {
    window.matchMedia ??= ((q: string) => ({matches: false, media: q, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}})) as unknown as typeof window.matchMedia;
});

import uPlot from 'uplot';

import {deNames, deTimeStamps, timeAxisFor} from './chartTime.js';

// what uPlot does with a stamp row: the default template, or default + rollover in mode 1
function label(row: (typeof deTimeStamps)[number], rollover: number | null, date: Date): string {
    const tpl = rollover === null ? row[1] : row[1] + (row[rollover] ?? '');
    return uPlot.fmtDate(tpl, deNames)(date);
}

describe('the chart time axis', () => {
    const at = new Date(2026, 9, 27, 14, 15, 7);
    const row = (incr: number) => deTimeStamps.find((r) => incr >= r[0])!;

    it('is German for de, uPlot default otherwise', () => {
        expect(timeAxisFor('de')?.stamps).toBe(deTimeStamps);
        expect(timeAxisFor('de-DE')?.names).toBe(deNames);
        expect(timeAxisFor('en')).toBeUndefined();
    });

    it('labels hours and minutes on the 24 h clock, the date as day.month.year', () => {
        expect(label(row(15 * 60), null, at)).toBe('14:15');
        expect(label(row(3600), null, at)).toBe('14:15');
        expect(label(row(3600), 2, at)).toBe('14:15\n27.10.26'); // a new year
        expect(label(row(3600), 4, at)).toBe('14:15\n27.10.'); // a new day
    });

    it('labels days and months in German', () => {
        expect(label(row(86400), null, at)).toBe('27.10.');
        expect(label(row(86400), 2, at)).toBe('27.10.\n2026');
        expect(label(row(86400 * 30), null, at)).toBe('Okt');
        expect(label(row(86400 * 30), null, new Date(2026, 2, 1))).toBe('Mär');
        expect(label(row(86400 * 400), null, at)).toBe('2026');
    });

    it('labels seconds with the time of the minute', () => {
        expect(label(row(5), null, at)).toBe(':07');
        expect(label(row(5), 6, at)).toBe(':07\n14:15');
    });

    it('has one row per uPlot increment, from years down, each with nine fields', () => {
        for (const r of deTimeStamps) expect(r).toHaveLength(9);
        const incrs = deTimeStamps.map((r) => r[0]);
        expect([...incrs].sort((a, b) => b - a)).toEqual(incrs);
    });
});
