// The pure part of the app: the API's shapes, the status of a device, formatting. No DOM, so it
// is tested directly.

export interface DeviceView {
    interface: string;
    address: string;
    name: string;
    rooms: string[];
    model: string;
    firmware?: string;
    power: string;
    voltage_dp?: string;
    lowbat_dp?: string;
    voltage: number | null;
    voltage_ts?: number;
    voltage_status?: string;
    low_bat: boolean | null;
    low_bat_since?: number;
    unreach: boolean | null;
    unreach_since?: number;
    last_report?: number;
    first_seen: number;
    profile?: Profile;
    /** The platform's picture of the model (/config/img/devices/250/...), absent without one. */
    image?: string;
    /** The device's battery lives; absent without an accepted change. */
    life?: DeviceLife;
}

export interface Slot {
    count: number;
    type: string;
    chemistry?: string;
}

export interface Profile {
    source: 'table' | 'override' | 'guess' | 'none';
    model?: string;
    power: string;
    slots: Slot[];
    fresh?: number;
    low?: number;
    step?: number;
    outdoor?: boolean;
    life_years?: number;
    confidence?: string;
}

export interface Evidence {
    triggers: string[];
    voltage_before?: number;
    max_before?: number;
    voltage_after?: number;
    voltage_confirmed?: number;
    confirmed_at?: number;
    low_bat_before?: boolean;
    booted?: boolean;
    unreach_gap_s?: number;
    silence_s?: number;
    fresh?: number;
    step?: number;
    window_s?: number;
    last_trigger: number;
    reason?: string;
}

export interface Battery {
    slot?: number;
    count?: number;
    type?: string;
    chemistry?: string;
    rechargeable?: boolean;
    vendor?: string;
    product?: string;
    price_cents?: number;
    lot?: string;
}

export interface Change {
    id: number;
    interface: string;
    address: string;
    name?: string;
    model?: string;
    at: number;
    kind: 'detected' | 'confirmed' | 'manual' | 'import';
    confidence?: '' | 'high' | 'medium' | 'low';
    rule?: string;
    status: 'candidate' | 'pending' | 'accepted' | 'rejected' | 'discarded';
    evidence?: Evidence;
    batteries: Battery[];
    note: string;
    source: string;
    external_id?: string;
    created_at: number;
    updated_at: number;
}

/** A device's battery lives as GET devices carries them. */
export interface DeviceLife {
    count: number;
    min_days?: number;
    max_days?: number;
    avg_days?: number;
    median_days?: number;
    running_since?: number;
    running_days?: number;
    hint?: 'short';
}

export interface LifeSummary {
    count: number;
    min_days?: number;
    max_days?: number;
    avg_days?: number;
    median_days?: number;
}

export interface Life {
    interface: string;
    address: string;
    model: string;
    change_id: number;
    from: number;
    to?: number;
    days: number;
    batteries: Battery[];
}

export interface ModelLife extends LifeSummary {
    model?: string;
    key?: string;
    vendor?: string;
    product?: string;
    type?: string;
    cost_per_year_cents?: number;
    priced_lives?: number;
}

export interface BatteryRow {
    key: string;
    vendor: string;
    product: string;
    type: string;
    total: number;
    changes: number;
    devices: number;
    models: string[];
    life: LifeSummary;
    avg_price_cents?: number;
    priced_total?: number;
    cost_per_year_cents?: number;
    priced_lives?: number;
    last_price_cents?: number;
    last_at?: number;
    per_model: ModelLife[];
}

export interface ModelRow {
    model: string;
    devices: number;
    life: LifeSummary;
    batteries: ModelLife[];
}

export interface Stats {
    now: number;
    devices: Record<string, DeviceLife>;
    lives: Life[];
    batteries: BatteryRow[];
    models: ModelRow[];
    types: {type: string; life: LifeSummary}[];
    year: {
        from: number;
        changes: number;
        batteries: number;
        by_type: Record<string, number>;
        cost_cents: number;
        priced_batteries: number;
        batteries_ever: number;
        first_change?: number;
    };
}

export interface StatsNames {
    [key: string]: {name: string; model: string; image?: string};
}

export interface Trend {
    mv_per_day: number;
    points: number;
    span_days: number;
    reach_low_at?: number;
}

export interface Forecast {
    expected_days?: number;
    basis: 'own' | 'model' | 'table' | 'trend' | 'none';
    lives?: number;
    due_at?: number;
    days_left: number;
    state: 'low' | 'due' | 'soon' | 'ok' | 'unknown';
    trend?: Trend;
}

export interface DeviceForecast {
    interface: string;
    address: string;
    name: string;
    rooms: string[];
    model: string;
    image?: string;
    slots: Slot[];
    low_bat: boolean;
    voltage: number | null;
    forecast: Forecast;
}

export interface ShoppingItem {
    type: string;
    needed: number;
    stock: number;
    buy: number;
    devices: string[];
}

/** The due column: "in 3 Mon." / "überfällig (12 T.)" / "leer"; "" when unknown. */
export function dueText(f: Forecast | undefined, lang = 'en'): string {
    if (!f) return '';
    const de = lang === 'de';
    switch (f.state) {
        case 'low':
            return de ? 'leer' : 'low';
        case 'due':
            return (de ? 'überfällig ' : 'overdue ') + formatDays(-f.days_left, lang);
        case 'soon':
        case 'ok':
            return (de ? 'in ' : 'in ') + formatDays(f.days_left, lang);
        default:
            return '';
    }
}

export interface StorageHealth {
    state: 'ok' | 'degraded' | 'unavailable';
    error?: string;
    path: string;
    location: {location: string; folder?: string};
    pending: number;
    dropped: number;
    last_flush?: string;
    flushes: number;
    size_bytes: number;
    in_backup: boolean;
}

export interface Candidate {
    id: string;
    name: string;
    path: string;
    free_bytes: number;
    total_bytes: number;
    allowed: boolean;
    reason?: string;
    in_backup: boolean;
    current: boolean;
}

export type Status = 'low' | 'unreach' | 'external' | 'ok' | 'unknown';

const UNIT: Record<string, {y: string; mo: string; d: string}> = {
    de: {y: 'J.', mo: 'Mon.', d: 'T.'},
    en: {y: 'y', mo: 'mo', d: 'd'},
};

/** A duration in days as people say it: "1 J. 3 Mon.", "8 Mon.", "12 T." / "1 y 3 mo", "8 mo", "12 d". */
export function formatDays(days: number | undefined, lang = 'en'): string {
    if (days === undefined || !Number.isFinite(days)) return '';
    const u = UNIT[lang] ?? UNIT.en!;
    const d = Math.round(days);
    if (d < 31) return `${d} ${u.d}`;
    const months = Math.round(d / 30.4375);
    if (months < 12) return `${months} ${u.mo}`;
    const years = Math.floor(months / 12);
    const rest = months % 12;
    return rest === 0 ? `${years} ${u.y}` : `${years} ${u.y} ${rest} ${u.mo}`;
}

/** A range of days, compact where both ends share a unit: "9–19 Mon.", "12–30 T.", else "9 Mon. – 1 J. 7 Mon.". */
export function formatRange(min: number | undefined, max: number | undefined, lang = 'en'): string {
    if (min === undefined || max === undefined) return '';
    const u = UNIT[lang] ?? UNIT.en!;
    const a = Math.round(min);
    const b = Math.round(max);
    if (b < 31) return a === b ? `${a} ${u.d}` : `${a}–${b} ${u.d}`;
    const ma = Math.round(a / 30.4375);
    const mb = Math.round(b / 30.4375);
    if (a >= 31 && mb < 12) return ma === mb ? `${ma} ${u.mo}` : `${ma}–${mb} ${u.mo}`;
    return `${formatDays(min, lang)} – ${formatDays(max, lang)}`;
}

/** The life column: "Ø 14 Mon. (9–19 Mon., 3×)"; "" without a completed life. */
export function lifeText(life: Pick<DeviceLife, 'count' | 'min_days' | 'max_days' | 'avg_days'> | undefined, lang = 'en'): string {
    if (!life || life.count === 0 || life.avg_days === undefined) return '';
    if (life.count === 1) return `${formatDays(life.avg_days, lang)} (1×)`;
    return `Ø ${formatDays(life.avg_days, lang)} (${formatRange(life.min_days, life.max_days, lang)}, ${life.count}×)`;
}

/** A money amount in cents, "0,89 €" / "€0.89"; "" for none. */
export function formatCents(cents: number | undefined | null, lang = 'en'): string {
    if (cents === undefined || cents === null || !Number.isFinite(cents)) return '';
    return (cents / 100).toLocaleString(lang, {style: 'currency', currency: 'EUR', minimumFractionDigits: 2, maximumFractionDigits: 2});
}

/** The value of a price input for cents: "0,89" in German, "0.89" in English; "" for none. */
export function priceInput(cents: number | undefined | null, lang = 'en'): string {
    if (cents === undefined || cents === null || !Number.isFinite(cents)) return '';
    return (cents / 100).toLocaleString(lang, {minimumFractionDigits: 2, maximumFractionDigits: 2, useGrouping: false});
}

/**
 * Cents from what the user typed, "0,89", "0.89", "1", "1,5 €": the last comma or point is the
 * decimal mark. undefined for an empty field, NaN for something that is not a price.
 */
export function parsePrice(v: string): number | undefined {
    const s = v.replace(/[€\s]/g, '').trim();
    if (s === '') return undefined;
    if (!/^\d+([.,]\d{0,2})?$/.test(s)) return NaN;
    const m = /^(\d+)(?:[.,](\d{0,2}))?$/.exec(s)!;
    const frac = (m[2] ?? '').padEnd(2, '0');
    return Number(m[1]) * 100 + Number(frac);
}

/** Two letters of a model for the picture placeholder: "WR" for HmIP-WRC2. */
export function modelInitials(model: string): string {
    return model
        .replace(/^(HmIPW?|HM|HMW|ZEL)-?/i, '')
        .replace(/[^A-Za-z0-9]/g, '')
        .slice(0, 2)
        .toUpperCase();
}

/** The battery row's name: "Varta · Power on Demand · AA"; the unnamed one is "–". */
export function batteryName(b: {vendor?: string; product?: string; type?: string}, unnamed = '–'): string {
    const parts = [b.vendor, b.product].filter((x) => x && x.trim());
    return (parts.length ? parts.join(' · ') : unnamed) + (b.type ? ` · ${b.type}` : '');
}


/** The one word the status pill says, worst first: unreachable, low, externally powered, ok. */
export function statusOf(d: Pick<DeviceView, 'low_bat' | 'unreach' | 'voltage_status' | 'power' | 'voltage'>): Status {
    if (d.unreach === true) return 'unreach';
    if (d.low_bat === true) return 'low';
    if (d.voltage_status === 'EXTERNAL' || isMains(d.power)) return 'external';
    if (d.low_bat === false || d.voltage !== null) return 'ok';
    return 'unknown';
}

/** "2.90 V", "" for none. */
export function formatVoltage(v: number | null | undefined, lang = 'en'): string {
    if (v === null || v === undefined) return '';
    return `${v.toLocaleString(lang, {minimumFractionDigits: 2, maximumFractionDigits: 2})} V`;
}

/** A relative time: "3 h ago" / "vor 3 Std.", with the day for anything older than a week. */
export function relativeTime(ms: number | undefined, now: number, lang = 'en'): string {
    if (!ms) return '';
    const diff = ms - now;
    const abs = Math.abs(diff);
    const rtf = new Intl.RelativeTimeFormat(lang, {numeric: 'auto'});
    if (abs < 60_000) return rtf.format(Math.round(diff / 1000), 'second');
    if (abs < 3_600_000) return rtf.format(Math.round(diff / 60_000), 'minute');
    if (abs < 86_400_000) return rtf.format(Math.round(diff / 3_600_000), 'hour');
    if (abs < 7 * 86_400_000) return rtf.format(Math.round(diff / 86_400_000), 'day');
    return new Date(ms).toLocaleDateString(lang, {year: 'numeric', month: '2-digit', day: '2-digit'});
}

/** "12.3 MB", "512 kB". */
export function formatBytes(n: number): string {
    if (!Number.isFinite(n)) return '';
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} kB`;
    if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`;
    return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`;
}

/** The sort of the device list: by status (worst first), then by name. */
export function compareDevices(a: DeviceView, b: DeviceView): number {
    const rank: Record<Status, number> = {unreach: 0, low: 1, unknown: 2, ok: 3, external: 4};
    const d = rank[statusOf(a)] - rank[statusOf(b)];
    if (d !== 0) return d;
    return (a.name || a.address).localeCompare(b.name || b.address);
}

/** Powered from outside: the battery statistics leave such a device out. */
export function isMains(power: string | undefined): boolean {
    return power === 'external' || power === 'mains' || power === 'bus' || power === 'host';
}

/** "2× AAA", "1× CR2032 + 2× AA"; "" for none. */
export function describeSlots(slots: Slot[] | undefined): string {
    if (!slots || slots.length === 0) return '';
    return slots.map((s) => `${s.count}× ${s.type}`).join(' + ');
}

/** The voltage story of a detection: "2.80 V → 3.10 V, 3.08 V after 24 h". */
export function evidenceSummary(ev: Evidence | undefined, lang = 'en', after = 'after'): string {
    if (!ev) return '';
    const parts: string[] = [];
    if (ev.voltage_before !== undefined && ev.voltage_after !== undefined) {
        parts.push(`${formatVoltage(ev.voltage_before, lang)} → ${formatVoltage(ev.voltage_after, lang)}`);
    } else if (ev.voltage_after !== undefined) {
        parts.push(formatVoltage(ev.voltage_after, lang));
    }
    if (ev.voltage_confirmed !== undefined && ev.window_s) {
        parts.push(`${formatVoltage(ev.voltage_confirmed, lang)} ${after} ${Math.round(ev.window_s / 3600)} h`);
    }
    return parts.join(', ');
}

/** A date and time in the user's language, "27.10.2026, 11:26". */
export function formatDateTime(ms: number, lang = 'en'): string {
    return new Date(ms).toLocaleString(lang, {year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit'});
}

/** The value of a datetime-local input for ms, in local time. */
export function toLocalInput(ms: number): string {
    const d = new Date(ms);
    const p = (n: number) => String(n).padStart(2, '0');
    return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`;
}

/** ms from a datetime-local input's value (local time); NaN when empty or invalid. */
export function fromLocalInput(v: string): number {
    if (!v) return NaN;
    return new Date(v).getTime();
}
