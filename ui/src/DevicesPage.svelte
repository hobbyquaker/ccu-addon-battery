<script lang="ts">
    import {DataTable} from 'occulite-ui';
    import {i18n, t} from 'occulite-ui/i18n';
    import type {NoticesStore} from 'occulite-ui/notices';
    import type {DataTableColumn} from 'occulite-ui/table';

    import {get} from './api.js';
    import {compareDevices, formatVoltage, relativeTime, statusOf, type DeviceView} from './model.js';

    let {notices}: {notices: NoticesStore} = $props();

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
    const columns = $derived<DataTableColumn<DeviceView>[]>([
        {key: 'name', label: t('Name'), width: 200, copy: 'name'},
        {key: 'rooms', label: t('Room'), width: 120, value: (d) => d.rooms.join(', ')},
        {key: 'model', label: t('Model'), width: 120},
        {key: 'address', label: t('Address'), width: 130, mono: true, copy: 'address'},
        {key: 'status', label: t('Status'), width: 120, fixed: true, value: (d) => t('status.' + statusOf(d)), align: 'center'},
        {key: 'voltage', label: t('Voltage'), width: 80, align: 'right', value: (d) => (d.voltage === null ? '' : d.voltage)},
        {key: 'low_bat_since', label: t('Low since'), width: 110, value: (d) => (d.low_bat ? relativeTime(d.low_bat_since, now, lang) : '')},
        {key: 'last_report', label: t('Last report'), width: 110, value: (d) => relativeTime(d.last_report, now, lang)},
    ]);
</script>

{#if error}
    <div class="bat-banner" role="alert">
        {t('Could not load: {error}', {error})}
        <button class="bat-muted" type="button" onclick={load}>{t('Reload')}</button>
    </div>
{/if}
{#if loading}
    <p class="bat-empty">{t('Loading…')}</p>
{:else}
    <div class="bat-table">
        <DataTable
            rows={devices}
            {columns}
            getId={(d) => `${d.interface}.${d.address}`}
            columnFilterRow={true}
            caption={t('Devices')}
            emptyText={t('No battery devices found yet.')}
            countText={t('{count} devices', {count: devices.length}, devices.length)}
            showingText={(shown, total) => t('Showing {shown} of {total}', {shown, total})}
            tableId="battery-devices"
            testId="devices"
        >
            {#snippet cell(row, column)}
                {#if column.key === 'status'}
                    {@const s = statusOf(row)}
                    <span class="bat-pill {s}" title={t('status.' + s)}>{t('status.' + s)}</span>
                {:else if column.key === 'voltage'}
                    <span class="bat-mono" title={row.voltage_dp ?? t('No voltage')}>{formatVoltage(row.voltage, lang)}</span>
                {:else}
                    {column.value ? String(column.value(row) ?? '') : String((row as unknown as Record<string, unknown>)[column.key] ?? '')}
                {/if}
            {/snippet}
        </DataTable>
    </div>
{/if}
