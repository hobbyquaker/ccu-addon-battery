<script lang="ts">
    // The Batteries tab: every battery (vendor, product line, size) ever recorded, with how many
    // went in, where, how long they lasted and what they cost - and, since a battery's life
    // depends on the device, the life per device model behind each row, plus a comparison: pick
    // a model (or a size) and rank the batteries used in it. The unnamed ones are one row.
    import {DataTable} from 'occulite-ui';
    import {i18n, t} from 'occulite-ui/i18n';
    import type {DataTableColumn} from 'occulite-ui/table';

    import {get} from './api.js';
    import {batteryName, formatCents, formatDays, type BatteryRow, type ModelLife, type Stats} from './model.js';

    let stats = $state<Stats | undefined>();
    let error = $state('');
    let loading = $state(true);
    let expanded = $state<string | undefined>();
    let compare = $state('');

    async function load() {
        try {
            const r = await get<{stats: Stats}>(`api/v1/stats?lang=${i18n.language}`);
            stats = r.stats;
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
    const rows = $derived(stats?.batteries ?? []);
    const columns = $derived<DataTableColumn<BatteryRow>[]>([
        {key: 'name', label: t('battery.name'), width: 240, value: (r) => batteryName(r, t('battery.unnamed'))},
        {key: 'total', label: t('battery.total'), width: 80, align: 'right'},
        {key: 'changes', label: t('battery.changes'), width: 80, align: 'right'},
        {key: 'devices', label: t('battery.devices'), width: 80, align: 'right'},
        {key: 'models', label: t('battery.models'), width: 160, value: (r) => r.models.join(', ')},
        {key: 'avg', label: t('battery.avgLife'), width: 110, align: 'right', value: (r) => (r.life.count ? r.life.avg_days : undefined), filterable: false},
        {key: 'range', label: t('battery.range'), width: 150, sortable: false, filterable: false},
        {key: 'price', label: t('battery.price'), width: 90, align: 'right', value: (r) => r.avg_price_cents, filterable: false},
        {key: 'perYear', label: t('battery.perYear'), width: 100, align: 'right', value: (r) => r.cost_per_year_cents, filterable: false},
        {key: 'actions', label: '', width: 110, fixed: true, minWidth: 110, keepMinWidth: true, sortable: false, filterable: false},
    ]);

    // the comparison: every model (and size) that has at least one completed life
    const compareOptions = $derived([
        ...(stats?.models ?? []).filter((m) => m.batteries.some((b) => b.count > 0)).map((m) => ({id: `model:${m.model}`, label: m.model})),
        ...(stats?.types ?? []).filter((x) => x.life.count > 0).map((x) => ({id: `type:${x.type}`, label: t('battery.sizeOption', {type: x.type})})),
    ]);
    const ranking = $derived<(ModelLife & {label: string})[]>(
        !stats || !compare
            ? []
            : compare.startsWith('model:')
              ? (stats.models.find((m) => `model:${m.model}` === compare)?.batteries ?? []).map((b) => ({...b, label: batteryName(b, t('battery.unnamed'))}))
              : stats.batteries
                    .filter((b) => `type:${b.type}` === compare && b.life.count > 0)
                    .map((b) => ({...b.life, key: b.key, cost_per_year_cents: b.cost_per_year_cents, priced_lives: b.priced_lives, label: batteryName(b, t('battery.unnamed'))}))
                    .sort((a, b) => (b.avg_days ?? 0) - (a.avg_days ?? 0)),
    );
    const rankMax = $derived(Math.max(1, ...ranking.map((r) => r.avg_days ?? 0)));
    const expandedRow = $derived(rows.find((r) => r.key === expanded));

    function rangeText(r: BatteryRow): string {
        if (!r.life.count) return '–';
        if (r.life.count === 1) return `${formatDays(r.life.avg_days, lang)} (1×)`;
        return `${formatDays(r.life.min_days, lang)} – ${formatDays(r.life.max_days, lang)} (${r.life.count}×)`;
    }
</script>

{#if error}
    <div class="bat-banner" role="alert">{t('Could not load: {error}', {error})}</div>
{:else if loading || !stats}
    <p class="bat-empty">{t('Loading…')}</p>
{:else if rows.length === 0}
    <p class="bat-empty">{t('battery.empty')}</p>
{:else}
    <p class="bat-muted bat-intro">{t('battery.intro')}</p>
    <div class="bat-table bat-table-short">
        <DataTable
            {rows}
            {columns}
            getId={(r) => r.key}
            columnFilterRow={true}
            caption={t('Batteries')}
            emptyText={t('battery.empty')}
            countText={t('{count} batteries', {count: rows.length}, rows.length)}
            showingText={(shown, total) => t('Showing {shown} of {total}', {shown, total})}
            tableId="battery-batteries"
            testId="batteries"
        >
            {#snippet cell(row, column)}
                {#if column.key === 'avg'}
                    <span title={rangeText(row)}>{row.life.count ? `Ø ${formatDays(row.life.avg_days, lang)}` : '–'}</span>
                {:else if column.key === 'range'}
                    {rangeText(row)}
                {:else if column.key === 'price'}
                    <span class="bat-mono" title={row.priced_total ? t('battery.priceOf', {n: row.priced_total}) : ''}>{formatCents(row.avg_price_cents, lang) || '–'}</span>
                {:else if column.key === 'perYear'}
                    <span class="bat-mono" title={row.priced_lives ? t('battery.perYearOf', {n: row.priced_lives}) : ''}>{row.priced_lives ? formatCents(row.cost_per_year_cents, lang) : '–'}</span>
                {:else if column.key === 'actions'}
                    <button type="button" class="bat-cell-button" onclick={() => (expanded = expanded === row.key ? undefined : row.key)} aria-expanded={expanded === row.key}>
                        {expanded === row.key ? t('battery.collapse') : t('battery.perModel')}
                    </button>
                {:else}
                    {column.value ? String(column.value(row) ?? '') : String((row as unknown as Record<string, unknown>)[column.key] ?? '')}
                {/if}
            {/snippet}
        </DataTable>
    </div>

    {#if expandedRow}
        <section class="bat-card" data-testid="battery-per-model">
            <h2>{batteryName(expandedRow, t('battery.unnamed'))}: {t('battery.perModel.title')}</h2>
            {#if expandedRow.per_model.length === 0}
                <p class="bat-muted">{t('battery.perModel.none')}</p>
            {:else}
                <table class="bat-plain">
                    <thead><tr><th>{t('Model')}</th><th>{t('battery.avgLife')}</th><th>{t('battery.range')}</th><th>{t('battery.perYear')}</th></tr></thead>
                    <tbody>
                        {#each expandedRow.per_model as m (m.model)}
                            <tr>
                                <td>{m.model || t('profile.unknown')}</td>
                                <td>Ø {formatDays(m.avg_days, lang)}</td>
                                <td>{m.count === 1 ? '(1×)' : `${formatDays(m.min_days, lang)} – ${formatDays(m.max_days, lang)} (${m.count}×)`}</td>
                                <td class="bat-mono">{m.priced_lives ? formatCents(m.cost_per_year_cents, lang) : '–'}</td>
                            </tr>
                        {/each}
                    </tbody>
                </table>
            {/if}
        </section>
    {/if}

    <section class="bat-card" data-testid="battery-compare">
        <h2>{t('battery.compare.title')}</h2>
        <p class="bat-muted">{t('battery.compare.intro')}</p>
        <div class="bat-form">
            <label for="bat-compare">{t('battery.compare.pick')}</label>
            <select id="bat-compare" class="hmm-select" bind:value={compare}>
                <option value="">–</option>
                {#each compareOptions as o (o.id)}<option value={o.id}>{o.label}</option>{/each}
            </select>
        </div>
        {#if compare && ranking.length === 0}
            <p class="bat-muted">{t('battery.compare.none')}</p>
        {:else if ranking.length}
            <ul class="bat-strips">
                {#each ranking as r (r.key)}
                    <li class="bat-strip">
                        <span class="bat-strip-label"><span class="bat-truncate" title={r.label}>{r.label}</span></span>
                        <span class="bat-strip-bar">
                            <span class="bat-strip-fill" style:width={`${((r.avg_days ?? 0) / rankMax) * 100}%`}></span>
                        </span>
                        <span class="bat-strip-value">
                            Ø {formatDays(r.avg_days, lang)} <span class="bat-muted">({r.count}×)</span>
                            {#if r.priced_lives}<span class="bat-mono"> · {formatCents(r.cost_per_year_cents, lang)}/{t('battery.year')}</span>{/if}
                        </span>
                    </li>
                {/each}
            </ul>
            <p class="bat-muted">{t('battery.compare.note')}</p>
        {/if}
    </section>
{/if}
