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

/** The one word the status pill says, worst first: unreachable, low, externally powered, ok. */
export function statusOf(d: Pick<DeviceView, 'low_bat' | 'unreach' | 'voltage_status' | 'power' | 'voltage'>): Status {
    if (d.unreach === true) return 'unreach';
    if (d.low_bat === true) return 'low';
    if (d.voltage_status === 'EXTERNAL' || d.power === 'external' || d.power === 'mains') return 'external';
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
