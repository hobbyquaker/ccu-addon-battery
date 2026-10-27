<script lang="ts">
    // The inbox: every detection waits here for the user - yes, no, or another day. Nothing the
    // detector finds is booked on its own. Below it, the changes recorded so far.
    import {DataTable} from 'occulite-ui';
    import {i18n, t} from 'occulite-ui/i18n';
    import type {NoticesStore} from 'occulite-ui/notices';
    import type {DataTableColumn} from 'occulite-ui/table';

    import ChangeDialog from './ChangeDialog.svelte';
    import {get, patch} from './api.js';
    import {evidenceSummary, formatDateTime, type Change} from './model.js';

    let {notices, onchanged}: {notices: NoticesStore; onchanged?: () => void} = $props();

    let pending = $state<Change[]>([]);
    let recorded = $state<Change[]>([]);
    let loading = $state(true);
    let error = $state('');
    let busy = $state(0);
    let redate = $state<Change | undefined>();
    let redateOpen = $state(false);

    async function load() {
        try {
            const [p, r] = await Promise.all([
                get<{changes: Change[]}>(`api/v1/changes?status=pending&lang=${i18n.language}`),
                get<{changes: Change[]}>(`api/v1/changes?status=accepted&limit=500&lang=${i18n.language}`),
            ]);
            pending = p.changes;
            recorded = r.changes;
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
        const id = setInterval(load, 60_000);
        return () => clearInterval(id);
    });

    async function decide(c: Change, body: {status: string; at?: number}) {
        busy = c.id;
        try {
            await patch(`api/v1/changes/${c.id}`, body);
            notices.push('info', body.status === 'accepted' ? t('inbox.confirmed', {name: c.name || c.address}) : t('inbox.rejected', {name: c.name || c.address}));
            redateOpen = false;
            await load();
            onchanged?.();
        } catch (e) {
            notices.push('error', t('inbox.failed', {error: (e as Error).message}));
        } finally {
            busy = 0;
        }
    }

    const lang = $derived(i18n.language);
    const columns = $derived<DataTableColumn<Change>[]>([
        {key: 'at', label: t('change.at'), width: 140, value: (c) => c.at},
        {key: 'name', label: t('Name'), width: 200, value: (c) => c.name || c.address},
        {key: 'model', label: t('Model'), width: 120},
        {key: 'kind', label: t('change.kind'), width: 110, fixed: true, value: (c) => t('kind.' + c.kind)},
        {key: 'batteries', label: t('Batteries'), width: 160, value: (c) => batteriesText(c)},
        {key: 'note', label: t('change.note'), width: 200},
    ]);

    function batteriesText(c: Change): string {
        return c.batteries
            .map((b) => [b.count ? `${b.count}×` : '', b.type ?? '', b.vendor ?? '', b.product ?? ''].filter(Boolean).join(' '))
            .filter(Boolean)
            .join(' + ');
    }

    function triggersText(c: Change): string {
        return (c.evidence?.triggers ?? []).map((r) => t('rule.' + r)).join(', ');
    }
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
    <section class="bat-inbox" aria-label={t('Inbox')}>
        <h2>{t('inbox.title')}</h2>
        <p class="bat-muted">{t('inbox.intro')}</p>
        {#if pending.length === 0}
            <p class="bat-empty">{t('inbox.empty')}</p>
        {:else}
            <ul class="bat-cards" data-testid="inbox">
                {#each pending as c (c.id)}
                    <li class="bat-card bat-detection">
                        <div class="bat-detection-head">
                            <strong class="bat-truncate" title={c.name || c.address}>{c.name || c.address}</strong>
                            <span class="bat-pill conf-{c.confidence || 'low'}" title={t('confidence.' + (c.confidence || 'low'))}>{t('confidence.' + (c.confidence || 'low'))}</span>
                        </div>
                        <div class="bat-muted">{c.model} · <span class="bat-mono">{c.address}</span></div>
                        <p>{t('inbox.question', {when: formatDateTime(c.at, lang)})}</p>
                        <p class="bat-muted">
                            {triggersText(c)}{#if evidenceSummary(c.evidence, lang)}; {evidenceSummary(c.evidence, lang, t('inbox.after'))}{/if}
                            {#if c.evidence?.reason === 'unconfirmed'}<br />{t('inbox.unconfirmed')}{/if}
                        </p>
                        <div class="bat-actions">
                            <button type="button" class="primary" disabled={busy !== 0} onclick={() => decide(c, {status: 'accepted'})}>{t('inbox.confirm')}</button>
                            <button type="button" disabled={busy !== 0} onclick={() => ((redate = c), (redateOpen = true))}>{t('inbox.otherDate')}</button>
                            <button type="button" disabled={busy !== 0} onclick={() => decide(c, {status: 'rejected'})}>{t('inbox.reject')}</button>
                        </div>
                    </li>
                {/each}
            </ul>
        {/if}
    </section>

    <h2 class="bat-section">{t('changes.title')}</h2>
    <div class="bat-table">
        <DataTable
            rows={recorded}
            {columns}
            getId={(c) => String(c.id)}
            columnFilterRow={true}
            caption={t('changes.title')}
            emptyText={t('changes.empty')}
            countText={t('{count} changes', {count: recorded.length}, recorded.length)}
            showingText={(shown, total) => t('Showing {shown} of {total}', {shown, total})}
            tableId="battery-changes"
            testId="changes"
        >
            {#snippet cell(row, column)}
                {#if column.key === 'at'}
                    {formatDateTime(row.at, lang)}
                {:else if column.key === 'kind'}
                    <span class="bat-pill kind" title={t('kind.' + row.kind)}>{t('kind.' + row.kind)}</span>
                {:else}
                    {column.value ? String(column.value(row) ?? '') : String((row as unknown as Record<string, unknown>)[column.key] ?? '')}
                {/if}
            {/snippet}
        </DataTable>
    </div>
{/if}

<ChangeDialog
    bind:open={redateOpen}
    mode="redate"
    device={redate ? {name: redate.name ?? '', address: redate.address, model: redate.model ?? '', profile: undefined} : undefined}
    at={redate?.at ?? Date.now()}
    busy={busy !== 0}
    onsubmit={(r) => redate && decide(redate, {status: 'accepted', at: r.at})}
/>
