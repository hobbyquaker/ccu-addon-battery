<script lang="ts">
    // The Devices tab: every battery device with its picture, status, batteries, voltage and how
    // long its batteries lasted so far - sortable by the mean life, shortest first, to find the
    // battery-hungry ones. A row's name opens the device's detail; "Changed..." records a change.
    import {DataTable} from 'occulite-ui';
    import {i18n, t} from 'occulite-ui/i18n';
    import type {NoticesStore} from 'occulite-ui/notices';
    import type {DataTableColumn} from 'occulite-ui/table';

    import ChangeDialog, {type EditorResult} from './ChangeDialog.svelte';
    import DevicePicture from './DevicePicture.svelte';
    import {get, post} from './api.js';
    import {compareDevices, describeSlots, dueText, formatDateTime, formatDays, formatVoltage, isMains, lifeText, relativeTime, statusOf, type DeviceForecast, type DeviceView, type Forecast} from './model.js';

    let {
        notices,
        inbox = 0,
        onshowinbox,
        onopen,
        onchanged,
    }: {notices: NoticesStore; inbox?: number; onshowinbox?: () => void; onopen?: (d: DeviceView) => void; onchanged?: () => void} = $props();

    // mains, bus and externally powered devices carry a low-battery flag too; they are left out
    // unless asked for
    let showMains = $state(false);
    let recordFor = $state<DeviceView | undefined>();
    let recordOpen = $state(false);
    let saving = $state(false);

    async function record(r: EditorResult) {
        const d = recordFor;
        if (!d) return;
        saving = true;
        try {
            const out = await post<{status: string}>('api/v1/changes', {device: d.address, interface: d.interface, at: r.at, note: r.note, batteries: r.batteries, source: 'manual'});
            notices.push('info', t('change.record.' + out.status, {name: d.name || d.address}));
            recordOpen = false;
            await load();
            onchanged?.();
        } catch (e) {
            notices.push('error', t('change.record.failed', {error: (e as Error).message}));
        } finally {
            saving = false;
        }
    }

    let devices = $state<DeviceView[]>([]);
    let forecasts = $state<Record<string, Forecast>>({});
    let loading = $state(true);
    let error = $state('');
    let now = $state(Date.now());

    async function load() {
        try {
            const [r, f] = await Promise.all([get<{devices: DeviceView[]}>(`api/v1/devices?lang=${i18n.language}`), get<{devices: DeviceForecast[]}>(`api/v1/forecast?lang=${i18n.language}`)]);
            devices = r.devices.sort(compareDevices);
            const map: Record<string, Forecast> = {};
            for (const d of f.devices) map[`${d.interface}.${d.address}`] = d.forecast;
            forecasts = map;
            error = '';
        } catch (e) {
            error = (e as Error).message;
        } finally {
            loading = false;
            now = Date.now();
        }
    }

    $effect(() => {
        void i18n.language; // names and rooms come in the page's language
        load();
        const id = setInterval(load, 30_000);
        return () => clearInterval(id);
    });

    const lang = $derived(i18n.language);
    const shown = $derived(showMains ? devices : devices.filter((d) => !isMains(d.power)));
    const hiddenMains = $derived(devices.length - devices.filter((d) => !isMains(d.power)).length);
    const columns = $derived<DataTableColumn<DeviceView>[]>([
        {key: 'image', label: '', width: 32, fixed: true, sortable: false, filterable: false, align: 'center'},
        {key: 'name', label: t('Name'), width: 190, copy: 'name'},
        {key: 'rooms', label: t('Room'), width: 110, value: (d) => d.rooms.join(', ')},
        {key: 'model', label: t('Model'), width: 120},
        {key: 'status', label: t('Status'), width: 120, fixed: true, value: (d) => t('status.' + statusOf(d)), align: 'center'},
        {key: 'batteries', label: t('Batteries'), width: 100, value: (d) => describeSlots(d.profile?.slots) || (d.profile?.source === 'none' ? t('profile.unknown') : '')},
        {key: 'voltage', label: t('Voltage'), width: 76, align: 'right', value: (d) => (d.voltage === null ? '' : d.voltage)},
        // sortable by the mean life (shortest first); devices without a completed life sort last
        {key: 'life', label: t('life.column'), width: 210, value: (d) => (d.life?.count ? d.life.avg_days : undefined), filterable: false},
        {key: 'running', label: t('life.running'), width: 90, value: (d) => d.life?.running_days, filterable: false},
        // sortable by the days left; the low ones first, the unknown last
        {key: 'due', label: t('forecast.column'), width: 136, fixed: true, value: (d) => dueSort(forecasts[`${d.interface}.${d.address}`]), filterable: false, align: 'center'},
        {key: 'last_report', label: t('Last report'), width: 100, value: (d) => relativeTime(d.last_report, now, lang)},
        {key: 'actions', label: '', width: 140, fixed: true, minWidth: 140, keepMinWidth: true, sortable: false, filterable: false},
    ]);

    function dueSort(f: Forecast | undefined): number | undefined {
        if (!f || f.state === 'unknown') return undefined;
        if (f.state === 'low') return -1e9;
        return f.days_left;
    }

    function dueTitle(f: Forecast | undefined): string {
        if (!f || f.state === 'unknown') return t('forecast.unknown');
        const parts = [t('forecast.basis.' + f.basis, {lives: f.lives ?? 0, days: formatDays(f.expected_days, lang)})];
        if (f.due_at) parts.push(t('forecast.dueOn', {when: formatDateTime(f.due_at, lang)}));
        if (f.trend?.reach_low_at) parts.push(t('forecast.trend', {mv: f.trend.mv_per_day, when: formatDateTime(f.trend.reach_low_at, lang)}));
        return parts.join('\n');
    }

    function lifeTitle(d: DeviceView): string {
        const l = d.life;
        if (!l) return t('life.none');
        const parts: string[] = [];
        if (l.count) parts.push(t('life.tooltip', {avg: formatDays(l.avg_days, lang), min: formatDays(l.min_days, lang), max: formatDays(l.max_days, lang), median: formatDays(l.median_days, lang), count: l.count}));
        else parts.push(t('life.none'));
        if (l.running_days !== undefined) parts.push(t('life.runningSince', {days: formatDays(l.running_days, lang)}));
        if (l.hint === 'short') parts.push(t('life.hint.short'));
        return parts.join('\n');
    }
</script>

{#if error}
    <div class="bat-banner" role="alert">
        {t('Could not load: {error}', {error})}
        <button class="bat-muted" type="button" onclick={load}>{t('Reload')}</button>
    </div>
{/if}
{#if inbox > 0}
    <div class="bat-banner info" role="status">
        {t('inbox.waiting', {count: inbox}, inbox)}
        <button type="button" class="bat-link" onclick={() => onshowinbox?.()}>{t('inbox.open')}</button>
    </div>
{/if}
{#if loading}
    <p class="bat-empty">{t('Loading…')}</p>
{:else}
    <div class="bat-toolbar">
        <label><input type="checkbox" bind:checked={showMains} /> {t('devices.showMains', {count: hiddenMains})}</label>
    </div>
    <div class="bat-table">
        <DataTable
            rows={shown}
            {columns}
            getId={(d) => `${d.interface}.${d.address}`}
            columnFilterRow={true}
            caption={t('Devices')}
            emptyText={t('No battery devices found yet.')}
            countText={t('{count} devices', {count: shown.length}, shown.length)}
            showingText={(shown, total) => t('Showing {shown} of {total}', {shown, total})}
            tableId="battery-devices"
            testId="devices"
        >
            {#snippet cell(row, column)}
                {#if column.key === 'image'}
                    <DevicePicture model={row.model} src={row.image} size={20} />
                {:else if column.key === 'name'}
                    <button type="button" class="bat-link bat-truncate" title={t('device.open')} onclick={() => onopen?.(row)}>{row.name || row.address}</button>
                {:else if column.key === 'status'}
                    {@const s = statusOf(row)}
                    <span class="bat-pill {s}" title={t('status.' + s)}>{t('status.' + s)}</span>
                {:else if column.key === 'actions'}
                    {#if !isMains(row.power)}
                        <button type="button" class="bat-cell-button" title={t('change.record.title')} onclick={() => ((recordFor = row), (recordOpen = true))}>{t('change.record.button')}</button>
                    {/if}
                {:else if column.key === 'batteries'}
                    <span class={row.profile?.source === 'none' ? 'bat-muted' : ''} title={row.profile ? t('profile.' + row.profile.source) : ''}>{column.value?.(row)}</span>
                {:else if column.key === 'voltage'}
                    <span class="bat-mono" title={row.voltage_dp ?? t('No voltage')}>{formatVoltage(row.voltage, lang)}</span>
                {:else if column.key === 'life'}
                    <span class="bat-life bat-truncate" class:short={row.life?.hint === 'short'} title={lifeTitle(row)}>{lifeText(row.life, lang) || '–'}</span>
                {:else if column.key === 'due'}
                    {@const f = forecasts[`${row.interface}.${row.address}`]}
                    <span class="bat-pill fc-{f?.state ?? 'unknown'}" title={dueTitle(f)}>{dueText(f, lang) || '–'}</span>
                {:else if column.key === 'running'}
                    <span class="bat-life" class:short={row.life?.hint === 'short'} title={lifeTitle(row)}>{row.life?.running_days !== undefined ? formatDays(row.life.running_days, lang) : '–'}</span>
                {:else}
                    {column.value ? String(column.value(row) ?? '') : String((row as unknown as Record<string, unknown>)[column.key] ?? '')}
                {/if}
            {/snippet}
        </DataTable>
    </div>
{/if}

<ChangeDialog bind:open={recordOpen} mode="record" device={recordFor} at={Date.now()} busy={saving} onsubmit={record} />
