// The chart's time axis in the UI language. uPlot formats its x axis from a table of date
// templates (one row per tick increment, with the longer label where a year, month or day
// rolls over) and English month names; English keeps uPlot's own table, German gets the same
// table with day.month. order and the 24 h clock (14:15, 27.10.26). Pure, so it is tested
// directly; the chart passes it to uPlot as axes[0].values and fmtDate.

/** uPlot's DateNames: month and weekday names for the {MMMM} {MMM} {WWWW} {WWW} tokens. */
export interface DateNames {
    MMMM: string[];
    MMM: string[];
    WWWW: string[];
    WWW: string[];
}

export const deNames: DateNames = {
    MMMM: ['Januar', 'Februar', 'März', 'April', 'Mai', 'Juni', 'Juli', 'August', 'September', 'Oktober', 'November', 'Dezember'],
    MMM: ['Jan', 'Feb', 'Mär', 'Apr', 'Mai', 'Jun', 'Jul', 'Aug', 'Sep', 'Okt', 'Nov', 'Dez'],
    WWWW: ['Sonntag', 'Montag', 'Dienstag', 'Mittwoch', 'Donnerstag', 'Freitag', 'Samstag'],
    WWW: ['So', 'Mo', 'Di', 'Mi', 'Do', 'Fr', 'Sa'],
};

/** One row: [minimum increment in s, default, then on a new year, month, day, hour, minute,
 *  second (null: no change), mode 1 = the rollover template is appended to the default]. */
export type StampRow = [number, string, string | null, string | null, string | null, string | null, string | null, string | null, 0 | 1];

const s = 1;
const m = 60 * s;
const h = 60 * m;
const d = 24 * h;
const y = 365 * d;
const NL = '\n';
const dm = '{DD}.{MM}.';
const dmy = dm + '{YY}';
const hm = '{HH}:{mm}';

/** The German table, row for row uPlot's English one (x values in seconds). */
export const deTimeStamps: StampRow[] = [
    [y, '{YYYY}', null, null, null, null, null, null, 1],
    [d * 28, '{MMM}', NL + '{YYYY}', null, null, null, null, null, 1],
    [d, dm, NL + '{YYYY}', null, null, null, null, null, 1],
    [h, hm, NL + dmy, null, NL + dm, null, null, null, 1],
    [m, hm, NL + dmy, null, NL + dm, null, null, null, 1],
    [s, ':{ss}', NL + dmy + ' ' + hm, null, NL + dm + ' ' + hm, null, NL + hm, null, 1],
    [1e-3, ':{ss}.{fff}', NL + dmy + ' ' + hm, null, NL + dm + ' ' + hm, null, NL + hm, null, 1],
];

/** The axis settings for a language: undefined keeps uPlot's English defaults. */
export function timeAxisFor(lang: string): {stamps: StampRow[]; names: DateNames} | undefined {
    return lang.toLowerCase().startsWith('de') ? {stamps: deTimeStamps, names: deNames} : undefined;
}
