<script lang="ts">
    import {DataTable} from 'occulite-ui';
    import {i18n, t} from 'occulite-ui/i18n';
    import type {NoticesStore} from 'occulite-ui/notices';
    import type {DataTableColumn} from 'occulite-ui/table';

    import ChangeDialog from './ChangeDialog.svelte';
    import {get, post} from './api.js';
    import {compareDevices, describeSlots, formatVoltage, isMains, relativeTime, statusOf, type DeviceView} from './model.js';

    let {notices, inbox = 0, onshowinbox}: {notices: NoticesStore; inbox?: number; onshowinbox?: () => void} = $props();

    // mains, bus and externally powered devices carry a low-battery flag too; they are left out
    // unless asked for
    let showMains = $state(false);
    let recordFor = $state<DeviceView | undefined>();
    let recordOpen = $state(false);
    let saving = $state(false);

    async function record(r: {at: number; note: string; vendor: string}) {
        const d = recordFor;
        if (!d) return;
        saving = true;
        try {
            const batteries = (d.profile?.slots ?? []).map((s, i) => ({slot: i + 1, count: s.count, type: s.type, chemistry: s.chemistry, vendor: r.vendor || undefined}));
            const out = await post<{status: string}>('api/v1/changes', {device: d.address, interface: d.interface, at: r.at, note: r.note, batteries, source: 'manual'});
            notices.push('info', t('change.record.' + out.status, {name: d.name || d.address}));
            recordOpen = false;
        } catch (e) {
            notices.push('error', t('change.record.failed', {error: (e as Error).message}));
        } finally {
            saving = false;
        }
    }

    let devices = $state<DeviceView[]>([]);
    let loading = $state(true);
    let error = $state('');
    let now = $state(Date.now());

    async function load() {
        try {
            const r = await get<{devices: DeviceView[]}>(`api/v1/devices?lang=${i18n.language}`);
            devices = r.devices.sort(compareDevices);
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
        {key: 'name', label: t('Name'), width: 200, copy: 'name'},
        {key: 'rooms', label: t('Room'), width: 120, value: (d) => d.rooms.join(', ')},
        {key: 'model', label: t('Model'), width: 120},
        {key: 'address', label: t('Address'), width: 130, mono: true, copy: 'address'},
        {key: 'status', label: t('Status'), width: 120, fixed: true, value: (d) => t('status.' + statusOf(d)), align: 'center'},
        {key: 'batteries', label: t('Batteries'), width: 110, value: (d) => describeSlots(d.profile?.slots) || (d.profile?.source === 'none' ? t('profile.unknown') : '')},
        {key: 'voltage', label: t('Voltage'), width: 80, align: 'right', value: (d) => (d.voltage === null ? '' : d.voltage)},
        {key: 'low_bat_since', label: t('Low since'), width: 110, value: (d) => (d.low_bat ? relativeTime(d.low_bat_since, now, lang) : '')},
        {key: 'last_report', label: t('Last report'), width: 110, value: (d) => relativeTime(d.last_report, now, lang)},
        {key: 'actions', label: '', width: 120, fixed: true, minWidth: 120, keepMinWidth: true, sortable: false, filterable: false},
    ]);
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
                {#if column.key === 'status'}
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
                {:else}
                    {column.value ? String(column.value(row) ?? '') : String((row as unknown as Record<string, unknown>)[column.key] ?? '')}
                {/if}
            {/snippet}
        </DataTable>
    </div>
{/if}

<ChangeDialog bind:open={recordOpen} mode="record" device={recordFor} at={Date.now()} busy={saving} onsubmit={record} />
