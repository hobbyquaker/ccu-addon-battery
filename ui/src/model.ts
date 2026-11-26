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
    /** The voltage is at or below the profile's low level: the addon's own low-battery finding, beside the device's flag. */
    voltage_low?: boolean;
    unreach: boolean | null;
    unreach_since?: number;
    last_report?: number;
    first_seen: number;
    /** The user switched the device's service messages off in the CCU; never set on openccu-lite. */
    service_messages_off?: boolean;
    /** The user does not monitor the device ("do not monitor"): out of the default list, the forecast, the shopping list and the notices. */
    ignored?: boolean;
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
    source: 'table' | 'override' | 'guess' | 'datapoints' | 'none';
    model?: string;
    power: string;
    slots: Slot[];
    fresh?: number;
    low?: number;
    step?: number;
    outdoor?: boolean;
    life_years?: number;
    confidence?: string;
    /** The power was detected automatically: a battery-or-dc device found on its power supply by its voltage. */
    auto?: boolean;
    /** The table row's power before the overrides. */
    table_power?: string;
    /** The preset the slots come from (the table stand), absent otherwise. */
    preset?: string;
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
    /** A candidate still being checked: when its confirmation window ends (Unix ms). */
    check_until?: number;
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
    voltage_low?: boolean;
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
    /** The write interval in minutes; interval_fixed: the daemon's -flush flag sets it. */
    interval_minutes?: number;
    interval_fixed?: boolean;
}

/** The database's part in the system backup (GET storage → backup). */
export interface BackupStatus {
    /** system: the system takes the snapshot before every backup; daily: the addon keeps one. */
    mode: 'system' | 'daily';
    last?: string;
    error?: string;
    notice?: {at: string; label: string; path: string};
    /** daily mode: how many days the addon's copy may get old before it is written again (1 or 7). */
    copy_days?: number;
}

export interface Candidate {
    /** userfs, usb:<label>, usb-uuid:<uuid> (a stick without a label) or usb-path:<mount point> (the system tells neither). */
    id: string;
    /** The stick's label, "" without one. */
    name: string;
    /** What identifies a stick: label, uuid or path; absent for the user partition. */
    kind?: 'label' | 'uuid' | 'path';
    uuid?: string;
    path: string;
    free_bytes: number;
    total_bytes: number;
    allowed: boolean;
    reason?: string;
    in_backup: boolean;
    current: boolean;
}

/** Whether a storage location id names a USB stick (anything but the user partition). */
export function isStick(id: string): boolean {
    return id.startsWith('usb:') || id.startsWith('usb-uuid:') || id.startsWith('usb-path:');
}

/** The message that names a storage location, and its parameters: a stick by its label, its UUID or its mount point. */
export function locationText(id: string): {key: string; params: Record<string, string>} {
    if (id.startsWith('usb-uuid:')) return {key: 'storage.candidate.usbUuid', params: {uuid: id.slice(9)}};
    if (id.startsWith('usb-path:')) return {key: 'storage.candidate.usbPath', params: {path: id.slice(9)}};
    if (id.startsWith('usb:')) return {key: 'storage.candidate.usb', params: {label: id.slice(4)}};
    return {key: 'storage.candidate.userfs', params: {}};
}

export type Status = 'ignored' | 'low' | 'unreach' | 'external' | 'ok' | 'unknown';

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


/** The one word the status pill says, worst first: unreachable, low (the device's flag, or a voltage at or below its low level), externally powered, ok. */
export function statusOf(d: Pick<DeviceView, 'low_bat' | 'unreach' | 'voltage_status' | 'power' | 'voltage'> & {ignored?: boolean; voltage_low?: boolean}): Status {
    if (d.ignored === true) return 'ignored';
    if (d.unreach === true) return 'unreach';
    if (d.low_bat === true || d.voltage_low === true) return 'low';
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
    const rank: Record<Status, number> = {unreach: 0, low: 1, unknown: 2, ok: 3, external: 4, ignored: 5};
    const d = rank[statusOf(a)] - rank[statusOf(b)];
    if (d !== 0) return d;
    return (a.name || a.address).localeCompare(b.name || b.address);
}

/** Powered from outside: the battery statistics leave such a device out. */
export function isMains(power: string | undefined): boolean {
    return power === 'external' || power === 'mains' || power === 'bus' || power === 'host';
}

/** Which filters of the device list the system can serve (GET devices' `filters`). */
export interface DeviceFilterCaps {
    /** The system has a per-device switch for service messages (a CCU3, OpenCCU, piVCCU; not openccu-lite). */
    service_messages: boolean;
}

/** The device list's filter, as the user set it; everything off shows what the list always showed. */
export interface DeviceFilter {
    /** Show mains, bus and externally powered devices too. */
    showMains: boolean;
    /** Hide the devices that are unreachable now (UNREACH). */
    hideUnreach: boolean;
    /** Hide the devices whose service messages are switched off in the CCU. */
    hideServiceOff: boolean;
    /** Show the devices the user does not monitor too. */
    showIgnored: boolean;
}

export const DEFAULT_DEVICE_FILTER: DeviceFilter = {showMains: false, hideUnreach: false, hideServiceOff: false, showIgnored: false};

/** The devices the filter leaves; a filter the system cannot serve hides nothing. */
export function filterDevices(devices: DeviceView[], f: DeviceFilter, caps: DeviceFilterCaps): DeviceView[] {
    return devices.filter(
        (d) =>
            (f.showIgnored || d.ignored !== true) &&
            (f.showMains || !isMains(d.power)) &&
            !(f.hideUnreach && d.unreach === true) &&
            !(f.hideServiceOff && caps.service_messages && d.service_messages_off === true),
    );
}

const DEVICE_FILTER_KEY = 'battery.devices.filter';

type FilterStorage = Pick<Storage, 'getItem' | 'setItem'>;

function browserStorage(): FilterStorage | undefined {
    try {
        return globalThis.localStorage;
    } catch {
        return undefined; // storage switched off in the browser
    }
}

/** The remembered filter of this browser; the default when there is none or it cannot be read. */
export function loadDeviceFilter(storage: FilterStorage | undefined = browserStorage()): DeviceFilter {
    try {
        const v = JSON.parse(storage?.getItem(DEVICE_FILTER_KEY) ?? 'null') as Partial<DeviceFilter> | null;
        if (!v || typeof v !== 'object') return {...DEFAULT_DEVICE_FILTER};
        return {showMains: v.showMains === true, hideUnreach: v.hideUnreach === true, hideServiceOff: v.hideServiceOff === true, showIgnored: v.showIgnored === true};
    } catch {
        return {...DEFAULT_DEVICE_FILTER};
    }
}

/** Remembers the filter in this browser; without storage the choice lasts until the page is left. */
export function saveDeviceFilter(f: DeviceFilter, storage: FilterStorage | undefined = browserStorage()): void {
    try {
        storage?.setItem(DEVICE_FILTER_KEY, JSON.stringify(f));
    } catch {
        /* full or refused */
    }
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

/**
 * The check of a detection still being checked (a candidate): until when, and whether that time has
 * passed - a voltage device is decided by its first reading after the window, so it can stay a
 * little longer. undefined for any other change.
 */
export function checkState(c: Pick<Change, 'status' | 'check_until'>, now = Date.now()): {until: number; late: boolean} | undefined {
    if (c.status !== 'candidate' || !c.check_until) return undefined;
    return {until: c.check_until, late: now >= c.check_until};
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

// ---- the battery editor (the device page's correction of the battery table) -------------------

/** An override of the battery table, for one device or for every device of a model (the API's body). */
export interface OverrideBody {
    /** One of the table's presets (the table stand): its power and slots replace the layer below's. */
    preset?: string;
    power?: string;
    slots?: Slot[];
    chemistry?: string;
    rechargeable?: boolean;
    outdoor?: boolean;
}

/** What the layer below an override says: the table row for a model, the model's profile for a device. */
export interface Baseline {
    power: string;
    slots: Slot[];
    outdoor: boolean;
    /** The preset the layer below already uses ("" none). */
    preset: string;
}

/** A preset of the battery table (GET models/{model} → presets): a named power and slots, e.g. eQ-3's table stand. */
export interface PresetInfo {
    name: {de: string; en: string};
    label: {de: string; en: string};
    power: string;
    slots: Slot[];
    life_factor?: number;
}

export type Presets = Record<string, PresetInfo>;

/** A preset's text in the user's language. */
export function presetText(p: PresetInfo | undefined, what: 'name' | 'label', lang = 'en'): string {
    if (!p) return '';
    return lang === 'de' ? p[what].de : p[what].en;
}

/** The editor's fields. chemistry "" is the slot types' own. */
export interface ProfileForm {
    /** The preset the form holds ("" none); it counts only while power and slots are the preset's. */
    preset: string;
    power: string;
    slots: Slot[];
    chemistry: string;
    rechargeable: boolean;
    outdoor: boolean;
}

/** The powers the editor offers (the table's kinds and the power supply unit; "unknown" only shows itself). */
export const EDITOR_POWERS = ['battery', 'rechargeable', 'battery-or-dc', 'external', 'mains', 'bus', 'host'];

/** A baseline from a table row or a profile; none: the model is not in the table. */
export function baselineOf(x?: {power?: string; slots?: Slot[]; outdoor?: boolean; preset?: string} | null): Baseline {
    return {power: x?.power || 'unknown', slots: (x?.slots ?? []).map((s) => ({...s})), outdoor: !!x?.outdoor, preset: x?.preset ?? ''};
}

/**
 * The editor's fields: the override over its baseline. Where neither says anything (a model the
 * table lacks), what the device shows - a guessed cell count, mains by its datapoints - prefills
 * the fields, so saving confirms it.
 */
export function profileForm(base: Baseline, ov: OverrideBody | null | undefined, shown?: Profile, presets: Presets = {}): ProfileForm {
    const pr = ov?.preset ? presets[ov.preset] : undefined;
    let power = pr?.power || ov?.power || base.power;
    let slots = pr ? pr.slots : ov?.slots?.length ? ov.slots : base.slots;
    const preset = pr ? ov!.preset! : ov?.slots?.length ? '' : base.preset;
    if (power === 'unknown' && shown && shown.power !== 'unknown' && !shown.auto) power = shown.power;
    if (slots.length === 0 && shown?.slots?.length && (shown.source === 'guess' || base.power === 'unknown')) slots = shown.slots.map((s) => ({count: s.count, type: s.type}));
    return {
        preset,
        power,
        slots: slots.map((s) => ({...s})),
        chemistry: ov?.chemistry ?? '',
        rechargeable: !!ov?.rechargeable,
        outdoor: ov?.outdoor ?? base.outdoor,
    };
}

function slotsEqual(a: Slot[], b: Slot[]): boolean {
    return a.length === b.length && a.every((s, i) => s.count === b[i]!.count && s.type === b[i]!.type && (s.chemistry ?? '') === (b[i]!.chemistry ?? ''));
}

/**
 * The override the editor writes: only what differs from the baseline, so the table's own levels
 * stay wherever nothing was changed; null when nothing differs (the override is removed).
 */
export function overrideOf(form: ProfileForm, base: Baseline, presets: Presets = {}): OverrideBody | null {
    const o: OverrideBody = {};
    const preset = activePreset(form, presets);
    // a preset the layer below already uses is inherited, not written again
    if (preset && preset !== base.preset) o.preset = preset;
    if (!preset && form.power !== base.power && form.power !== 'unknown') o.power = form.power;
    // mains, a bus, the host or the power supply unit: the batteries do not matter
    if (powerTakesBatteries(form.power)) {
        if (!preset && form.slots.length > 0 && !slotsEqual(form.slots, base.slots)) o.slots = form.slots.map((s) => (s.chemistry ? {count: s.count, type: s.type, chemistry: s.chemistry} : {count: s.count, type: s.type}));
        if (form.chemistry) o.chemistry = form.chemistry;
        if (form.rechargeable) o.rechargeable = true;
    }
    if (form.outdoor !== base.outdoor) o.outdoor = form.outdoor;
    return Object.keys(o).length ? o : null;
}

/** The form's preset while power and slots are still the preset's ("" otherwise: the user changed them). */
export function activePreset(form: ProfileForm, presets: Presets): string {
    const p = form.preset ? presets[form.preset] : undefined;
    return p && p.power === form.power && slotsEqual(form.slots, p.slots) ? form.preset : '';
}

/** The form with a preset chosen: its power and slots (the chemistry, rechargeable and outdoor stay). */
export function applyPreset(form: ProfileForm, id: string, presets: Presets): ProfileForm {
    const p = presets[id];
    if (!p) return form;
    return {...form, preset: id, power: p.power, slots: p.slots.map((s) => ({...s}))};
}

/** The fields the API would refuse: a count outside 1-12, a type the table does not know. */
export function profileFormValid(form: ProfileForm, types: string[]): boolean {
    if (!powerTakesBatteries(form.power)) return true;
    return form.slots.every((s) => Number.isInteger(s.count) && s.count >= 1 && s.count <= 12 && types.includes(s.type));
}

/** Whether the power takes batteries at all (the slots are asked for). */
export function powerTakesBatteries(power: string): boolean {
    return power === 'battery' || power === 'battery-or-dc' || power === 'unknown' || power === 'rechargeable';
}

/** A row of the battery table, the fields the editor uses. */
export interface TableRow {
    power: string;
    slots?: Slot[];
    outdoor?: boolean;
    /** The presets the editor offers for the model (the table stand). */
    presets?: string[];
}

/** GET models/{model}: the row, the model's override and the model's profile (row + model override). */
export interface ModelInfo {
    model?: string;
    in_table: boolean;
    row?: TableRow;
    override?: OverrideBody;
    profile?: {power: string; slots: Slot[]; outdoor?: boolean; preset?: string};
    /** The presets the row offers and the override names, by id. */
    presets?: Presets;
}

/** Where a value of the device's profile comes from: this device's override, its model's, the table, the addon's own guess. */
export type Origin = 'device' | 'model' | 'table' | 'auto' | 'none';

export interface Origins {
    power: Origin;
    slots: Origin;
    chemistry: Origin;
    outdoor: Origin;
}

/**
 * Per value, the level it comes from - this device wins over its model, the model over the table
 * (the order the backend resolves in).
 */
export function originsOf(profile: Profile | undefined, devOv: OverrideBody | null | undefined, modelOv: OverrideBody | null | undefined, inTable: boolean): Origins {
    const below: Origin = inTable ? 'table' : profile && (profile.source === 'guess' || profile.source === 'datapoints') ? 'auto' : 'none';
    const pick = (has: (o: OverrideBody) => boolean, fallback: Origin): Origin => (devOv && has(devOv) ? 'device' : modelOv && has(modelOv) ? 'model' : fallback);
    const slots = (o: OverrideBody) => !!o.slots?.length || !!o.preset;
    const chem = (o: OverrideBody) => !!o.chemistry || !!o.rechargeable;
    return {
        power: pick((o) => !!o.power, profile?.auto ? 'auto' : below),
        slots: pick(slots, below),
        chemistry: pick(chem, inTable ? 'table' : 'none'),
        outdoor: pick((o) => o.outdoor !== undefined, inTable ? 'table' : 'none'),
    };
}

const CHEMISTRIES = ['alkaline', 'alkaline-button', 'lithium-coin', 'lithium', 'lithium-aa', 'nimh', 'lead'];

/** A chemistry's label in the user's language; an unknown one as it is. */
export function chemistryLabel(c: string, tr: (key: string) => string): string {
    return CHEMISTRIES.includes(c) ? tr('chemistry.' + c) : c;
}
