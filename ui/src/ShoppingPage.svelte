<script lang="ts">
    // The Shopping tab: which devices are low now or due within the horizon, what they need per
    // battery type against the stock at home, and what to buy - copyable as text. The stock and
    // the horizon are settings saved for next time.
    import {i18n, t} from 'occulite-ui/i18n';
    import type {NoticesStore} from 'occulite-ui/notices';

    import DevicePicture from './DevicePicture.svelte';
    import {get, put} from './api.js';
    import {describeSlots, formatDateTime, formatDays, type DeviceForecast, type ShoppingItem} from './model.js';

    let {notices, onopen}: {notices: NoticesStore; onopen?: (key: string) => void} = $props();

    let days = $state(90);
    let items = $state<ShoppingItem[]>([]);
    let devices = $state<DeviceForecast[]>([]);
    let stock = $state<Record<string, number>>({});
    let loading = $state(true);
    let error = $state('');
    let saving = $state(false);
    let settingsDays = $state(90);

    async function load(withDays?: number) {
        try {
            const q = withDays ? `?days=${withDays}&lang=${i18n.language}` : `?lang=${i18n.language}`;
            const r = await get<{days: number; stock: Record<string, number>; items: ShoppingItem[]; devices: DeviceForecast[]}>(`api/v1/shopping${q}`);
            days = r.days;
            if (!withDays) settingsDays = r.days;
            items = r.items;
            devices = r.devices;
            stock = {...r.stock};
            error = '';
        } catch (e) {
            error = (e as Error).message;
        } finally {
            loading = false;
        }
    }

    $effect(() => {
        void i18n.language;
        load();
    });

    const lang = $derived(i18n.language);

    async function saveStock() {
        saving = true;
        try {
            const clean: Record<string, number> = {};
            for (const [k, v] of Object.entries(stock)) if (v > 0) clean[k] = v;
            await put('api/v1/settings', {shopping: {days, stock: clean}});
            notices.push('info', t('shopping.saved'));
            settingsDays = days;
            await load(days);
        } catch (e) {
            notices.push('error', t('Could not save: {error}', {error: (e as Error).message}));
        } finally {
            saving = false;
        }
    }

    function stateText(d: DeviceForecast): string {
        const f = d.forecast;
        if (f.state === 'low') return t('forecast.low');
        if (f.state === 'due') return t('forecast.due', {days: Math.round(-f.days_left)});
        if (f.state === 'soon') return t('forecast.soon', {days: Math.round(f.days_left)});
        return '';
    }

    function basisText(d: DeviceForecast): string {
        const f = d.forecast;
        const parts = [t('forecast.basis.' + f.basis, {lives: f.lives ?? 0, days: formatDays(f.expected_days, lang)})];
        if (f.due_at) parts.push(t('forecast.dueOn', {when: formatDateTime(f.due_at, lang)}));
        if (f.trend?.reach_low_at) parts.push(t('forecast.trend', {mv: f.trend.mv_per_day, when: formatDateTime(f.trend.reach_low_at, lang)}));
        return parts.join('\n');
    }

    const listText = $derived(
        items
            .filter((i) => i.buy > 0)
            .map((i) => `${i.buy}× ${i.type}`)
            .join(', '),
    );

    async function copy() {
        const lines = items.filter((i) => i.buy > 0).map((i) => `${i.buy}× ${i.type} (${i.devices.join(', ')})`);
        try {
            await navigator.clipboard.writeText(lines.join('\n'));
            notices.push('info', t('shopping.copied'));
        } catch {
            notices.push('error', t('shopping.copyFailed'));
        }
    }
</script>

{#if error}
    <div class="bat-banner" role="alert">{t('Could not load: {error}', {error})}</div>
{:else if loading}
    <p class="bat-empty">{t('Loading…')}</p>
{:else}
    <div class="bat-stats">
        <section class="bat-card" data-testid="shopping-list">
            <h2>{t('shopping.title')}</h2>
            <div class="bat-form">
                <label for="bat-horizon">{t('shopping.horizon')}</label>
                <select id="bat-horizon" class="hmm-select" bind:value={days} onchange={() => load(days)}>
                    {#each [30, 60, 90, 180, 365] as d (d)}<option value={d}>{t('shopping.days', {days: d})}</option>{/each}
                </select>
            </div>
            {#if items.length === 0}
                <p class="bat-empty">{t('shopping.nothing', {days})}</p>
            {:else}
                <table class="bat-plain">
                    <thead><tr><th>{t('change.type')}</th><th>{t('shopping.needed')}</th><th>{t('shopping.stock')}</th><th>{t('shopping.buy')}</th><th>{t('Devices')}</th></tr></thead>
                    <tbody>
                        {#each items as it (it.type)}
                            <tr>
                                <td>{it.type}</td>
                                <td class="bat-num">{it.needed}</td>
                                <td><input class="hmm-input bat-stock-input" type="number" min="0" max="10000" bind:value={stock[it.type]} oninput={() => (it.buy = Math.max(0, it.needed - (stock[it.type] ?? 0)))} /></td>
                                <td class="bat-num"><strong>{it.buy}</strong></td>
                                <td class="bat-truncate">{it.devices.join(', ')}</td>
                            </tr>
                        {/each}
                    </tbody>
                </table>
                <p class="bat-muted">{listText ? t('shopping.summary', {list: listText}) : t('shopping.covered')}</p>
            {/if}
            <div class="bat-actions">
                <button type="button" class="primary" onclick={saveStock} disabled={saving}>{t('shopping.save')}</button>
                <button type="button" onclick={copy} disabled={!listText}>{t('shopping.copy')}</button>
            </div>
            <p class="bat-muted">{t('shopping.stockHint')}</p>
        </section>

        <section class="bat-card" data-testid="due-list">
            <h2>{t('shopping.due.title', {days})}</h2>
            {#if devices.length === 0}
                <p class="bat-empty">{t('shopping.due.none')}</p>
            {:else}
                <table class="bat-plain">
                    <thead><tr><th></th><th>{t('Name')}</th><th>{t('Model')}</th><th>{t('Batteries')}</th><th>{t('forecast.column')}</th></tr></thead>
                    <tbody>
                        {#each devices as d (d.interface + '.' + d.address)}
                            <tr>
                                <td><DevicePicture model={d.model} src={d.image} size={20} /></td>
                                <td><button type="button" class="bat-link" onclick={() => onopen?.(`${d.interface}.${d.address}`)}>{d.name || d.address}</button></td>
                                <td>{d.model}</td>
                                <td>{describeSlots(d.slots) || '–'}</td>
                                <td><span class="bat-pill fc-{d.forecast.state}" title={basisText(d)}>{stateText(d)}</span></td>
                            </tr>
                        {/each}
                    </tbody>
                </table>
            {/if}
        </section>
    </div>
{/if}
