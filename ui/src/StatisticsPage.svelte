<script lang="ts">
    // The Statistics tab: the last twelve months in numbers, the life per model and per size as
    // sorted strips (median with the range and the number of lives behind it), the devices with
    // the shortest lives. Plain bars on the kit's tokens - a few dozen rows need no chart library.
    import {i18n, t} from 'occulite-ui/i18n';

    import DevicePicture from './DevicePicture.svelte';
    import {get} from './api.js';
    import {formatCents, formatDays, type DeviceLife, type LifeSummary, type Stats, type StatsNames} from './model.js';

    let {onopen}: {onopen?: (key: string) => void} = $props();

    let stats = $state<Stats | undefined>();
    let names = $state<StatsNames>({});
    let error = $state('');
    let loading = $state(true);

    async function load() {
        try {
            const r = await get<{stats: Stats; names: StatsNames}>(`api/v1/stats?lang=${i18n.language}`);
            stats = r.stats;
            names = r.names;
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

    interface Strip {
        key: string;
        label: string;
        life: LifeSummary;
        sub?: string;
        image?: string;
        model?: string;
    }

    const models = $derived<Strip[]>(
        (stats?.models ?? [])
            .filter((m) => m.life.count > 0)
            .map((m) => ({key: m.model, label: m.model, life: m.life, sub: t('{count} devices', {count: m.devices}, m.devices)}))
            .sort((a, b) => (a.life.median_days ?? 0) - (b.life.median_days ?? 0)),
    );
    const types = $derived<Strip[]>(
        (stats?.types ?? []).filter((x) => x.life.count > 0).map((x) => ({key: x.type, label: x.type, life: x.life})).sort((a, b) => (a.life.median_days ?? 0) - (b.life.median_days ?? 0)),
    );
    const worst = $derived<(Strip & {dev: DeviceLife})[]>(
        Object.entries(stats?.devices ?? {})
            .filter(([, l]) => l.count > 0)
            .map(([key, l]) => ({key, label: names[key]?.name || key.split('.')[1] || key, model: names[key]?.model, image: names[key]?.image, life: l, dev: l, sub: names[key]?.model}))
            .sort((a, b) => (a.life.avg_days ?? 0) - (b.life.avg_days ?? 0))
            .slice(0, 10),
    );
    const maxDays = $derived(Math.max(1, ...models.map((m) => m.life.max_days ?? 0), ...types.map((m) => m.life.max_days ?? 0), ...worst.map((m) => m.life.max_days ?? 0)));
    const byType = $derived(Object.entries(stats?.year.by_type ?? {}).sort((a, b) => b[1] - a[1]));

    function pct(days: number | undefined): string {
        return `${Math.min(100, ((days ?? 0) / maxDays) * 100)}%`;
    }
</script>

{#if error}
    <div class="bat-banner" role="alert">{t('Could not load: {error}', {error})}</div>
{:else if loading || !stats}
    <p class="bat-empty">{t('Loading…')}</p>
{:else if stats.lives.length === 0}
    <p class="bat-empty">{t('stats.empty')}</p>
{:else}
    <div class="bat-stats">
        <section class="bat-card">
            <h2>{t('stats.year.title')}</h2>
            <div class="bat-tiles">
                <div class="bat-tile"><span class="bat-tile-value">{stats.year.batteries}</span><span class="bat-muted">{t('stats.year.batteries')}</span></div>
                <div class="bat-tile"><span class="bat-tile-value">{stats.year.changes}</span><span class="bat-muted">{t('stats.year.changes')}</span></div>
                <div class="bat-tile">
                    <span class="bat-tile-value">{stats.year.priced_batteries > 0 ? formatCents(stats.year.cost_cents, lang) : '–'}</span>
                    <span class="bat-muted">{stats.year.priced_batteries > 0 && stats.year.priced_batteries < stats.year.batteries ? t('stats.year.costPartial', {n: stats.year.priced_batteries}) : t('stats.year.cost')}</span>
                </div>
                <div class="bat-tile"><span class="bat-tile-value">{stats.year.batteries_ever}</span><span class="bat-muted">{t('stats.ever')}</span></div>
            </div>
            {#if byType.length}
                <p class="bat-muted">{t('stats.year.byType')}: {byType.map(([k, n]) => `${n}× ${k}`).join(', ')}</p>
            {/if}
        </section>

        <section class="bat-card">
            <h2>{t('stats.worst.title')}</h2>
            <p class="bat-muted">{t('stats.worst.intro')}</p>
            <ul class="bat-strips">
                {#each worst as s (s.key)}
                    <li class="bat-strip">
                        <span class="bat-strip-label">
                            <DevicePicture model={s.model ?? ''} src={s.image} size={28} />
                            <span class="bat-strip-names">
                                <button type="button" class="bat-link bat-truncate" title={s.label} onclick={() => onopen?.(s.key)}>{s.label}</button>
                                {#if s.sub}<span class="bat-muted bat-truncate" title={s.sub}>{s.sub}</span>{/if}
                            </span>
                        </span>
                        <span class="bat-strip-bar" title={t('life.tooltip', {avg: formatDays(s.life.avg_days, lang), min: formatDays(s.life.min_days, lang), max: formatDays(s.life.max_days, lang), median: formatDays(s.life.median_days, lang), count: s.life.count})}>
                            <span class="bat-strip-range" style:left={pct(s.life.min_days)} style:width={`calc(${pct(s.life.max_days)} - ${pct(s.life.min_days)})`}></span>
                            <span class="bat-strip-median" style:left={pct(s.life.avg_days)}></span>
                        </span>
                        <span class="bat-strip-value">Ø {formatDays(s.life.avg_days, lang)} <span class="bat-muted">({s.life.count}×)</span></span>
                    </li>
                {/each}
            </ul>
        </section>

        <section class="bat-card">
            <h2>{t('stats.models.title')}</h2>
            <p class="bat-muted">{t('stats.strips.intro')}</p>
            <ul class="bat-strips">
                {#each models as s (s.key)}
                    <li class="bat-strip">
                        <span class="bat-strip-label">
                            <span class="bat-strip-names">
                                <span class="bat-truncate" title={s.label}>{s.label}</span>
                                {#if s.sub}<span class="bat-muted bat-truncate" title={s.sub}>{s.sub}</span>{/if}
                            </span>
                        </span>
                        <span class="bat-strip-bar" title={t('life.tooltip', {avg: formatDays(s.life.avg_days, lang), min: formatDays(s.life.min_days, lang), max: formatDays(s.life.max_days, lang), median: formatDays(s.life.median_days, lang), count: s.life.count})}>
                            <span class="bat-strip-range" style:left={pct(s.life.min_days)} style:width={`calc(${pct(s.life.max_days)} - ${pct(s.life.min_days)})`}></span>
                            <span class="bat-strip-median" style:left={pct(s.life.median_days)}></span>
                        </span>
                        <span class="bat-strip-value">{formatDays(s.life.median_days, lang)} <span class="bat-muted">({s.life.count}×)</span></span>
                    </li>
                {/each}
            </ul>
        </section>

        <section class="bat-card">
            <h2>{t('stats.types.title')}</h2>
            <ul class="bat-strips">
                {#each types as s (s.key)}
                    <li class="bat-strip">
                        <span class="bat-strip-label"><span class="bat-truncate" title={s.label}>{s.label}</span></span>
                        <span class="bat-strip-bar" title={t('life.tooltip', {avg: formatDays(s.life.avg_days, lang), min: formatDays(s.life.min_days, lang), max: formatDays(s.life.max_days, lang), median: formatDays(s.life.median_days, lang), count: s.life.count})}>
                            <span class="bat-strip-range" style:left={pct(s.life.min_days)} style:width={`calc(${pct(s.life.max_days)} - ${pct(s.life.min_days)})`}></span>
                            <span class="bat-strip-median" style:left={pct(s.life.median_days)}></span>
                        </span>
                        <span class="bat-strip-value">{formatDays(s.life.median_days, lang)} <span class="bat-muted">({s.life.count}×)</span></span>
                    </li>
                {/each}
            </ul>
        </section>

        <p class="bat-muted">
            <a href="api/v1/export.csv" download>{t('stats.export.csv')}</a> · <a href="api/v1/export.json" download>{t('stats.export.json')}</a>
        </p>
    </div>
{/if}
