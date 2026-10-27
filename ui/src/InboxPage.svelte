<script lang="ts">
    // The inbox: every detection waits here for the user - yes (with the batteries that went in),
    // another day, or no. Nothing the detector finds is booked on its own. Below it, the changes
    // recorded so far, each editable and deletable.
    import {DataTable} from 'occulite-ui';
    import {i18n, t} from 'occulite-ui/i18n';
    import type {NoticesStore} from 'occulite-ui/notices';
    import type {DataTableColumn} from 'occulite-ui/table';

    import ChangeDeleteDialog from './ChangeDeleteDialog.svelte';
    import ChangeDialog, {type EditorResult} from './ChangeDialog.svelte';
    import DevicePicture from './DevicePicture.svelte';
    import {del, get, patch} from './api.js';
    import {batteryName, evidenceSummary, formatCents, formatDateTime, type Change, type DeviceView} from './model.js';

    let {notices, onchanged}: {notices: NoticesStore; onchanged?: () => void} = $props();

    let pending = $state<Change[]>([]);
    let recorded = $state<Change[]>([]);
    let devices = $state<Record<string, DeviceView>>({});
    let loading = $state(true);
    let error = $state('');
    let busy = $state(0);
    let editorOpen = $state(false);
    let editorMode = $state<'confirm' | 'edit'>('confirm');
    let editing = $state<Change | undefined>();
    let confirmDelete = $state<Change | undefined>();

    async function load() {
        try {
            const [p, r, d] = await Promise.all([
                get<{changes: Change[]}>(`api/v1/changes?status=pending&lang=${i18n.language}`),
                get<{changes: Change[]}>(`api/v1/changes?status=accepted&limit=500&lang=${i18n.language}`),
                get<{devices: DeviceView[]}>(`api/v1/devices?lang=${i18n.language}`),
            ]);
            pending = p.changes;
            recorded = r.changes;
            const map: Record<string, DeviceView> = {};
            for (const x of d.devices) map[`${x.interface}.${x.address}`] = x;
            devices = map;
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

    function deviceOf(c: Change): DeviceView | undefined {
        return devices[`${c.interface}.${c.address}`];
    }

    function editorDevice(c: Change | undefined) {
        if (!c) return undefined;
        const d = deviceOf(c);
        return d ?? {name: c.name ?? '', address: c.address, model: c.model ?? '', profile: undefined, image: undefined};
    }

    function openConfirm(c: Change) {
        editing = c;
        editorMode = 'confirm';
        editorOpen = true;
    }

    function openEdit(c: Change) {
        editing = c;
        editorMode = 'edit';
        editorOpen = true;
    }

    async function decide(c: Change, body: {status: string; at?: number; batteries?: unknown; note?: string}) {
        busy = c.id;
        try {
            await patch(`api/v1/changes/${c.id}`, body);
            notices.push('info', body.status === 'accepted' ? t('inbox.confirmed', {name: c.name || c.address}) : t('inbox.rejected', {name: c.name || c.address}));
            editorOpen = false;
            await load();
            onchanged?.();
        } catch (e) {
            notices.push('error', t('inbox.failed', {error: (e as Error).message}));
        } finally {
            busy = 0;
        }
    }

    async function save(r: EditorResult) {
        const c = editing;
        if (!c) return;
        if (editorMode === 'confirm') {
            await decide(c, {status: 'accepted', at: r.at, batteries: r.batteries, note: r.note});
            return;
        }
        busy = c.id;
        try {
            await patch(`api/v1/changes/${c.id}`, {at: r.at, batteries: r.batteries, note: r.note});
            notices.push('info', t('change.edit.saved'));
            editorOpen = false;
            await load();
            onchanged?.();
        } catch (e) {
            notices.push('error', t('change.record.failed', {error: (e as Error).message}));
        } finally {
            busy = 0;
        }
    }

    async function remove(c: Change) {
        busy = c.id;
        try {
            await del(`api/v1/changes/${c.id}`);
            notices.push('info', t('change.deleted'));
            await load();
            onchanged?.();
        } catch (e) {
            notices.push('error', t('change.record.failed', {error: (e as Error).message}));
        } finally {
            busy = 0;
        }
    }

    const lang = $derived(i18n.language);
    const columns = $derived<DataTableColumn<Change>[]>([
        {key: 'image', label: '', width: 32, fixed: true, sortable: false, filterable: false, align: 'center'},
        {key: 'at', label: t('change.at'), width: 130, value: (c) => c.at},
        {key: 'name', label: t('Name'), width: 180, value: (c) => c.name || c.address},
        {key: 'model', label: t('Model'), width: 110},
        {key: 'kind', label: t('change.kind'), width: 110, fixed: true, value: (c) => t('kind.' + c.kind)},
        {key: 'batteries', label: t('Batteries'), width: 200, value: (c) => batteriesText(c)},
        {key: 'cost', label: t('change.cost'), width: 80, align: 'right', value: (c) => cost(c)},
        {key: 'note', label: t('change.note'), width: 160},
        {key: 'actions', label: '', width: 150, fixed: true, minWidth: 150, keepMinWidth: true, sortable: false, filterable: false},
    ]);

    function batteriesText(c: Change): string {
        return c.batteries
            .map((b) => `${b.count ?? 1}× ${batteryName(b, t('battery.unnamed'))}${b.rechargeable ? ' ⟳' : ''}`)
            .filter(Boolean)
            .join(' + ');
    }

    function cost(c: Change): number | undefined {
        let sum = 0;
        let any = false;
        for (const b of c.batteries) {
            if (b.price_cents !== undefined) {
                sum += (b.count ?? 1) * b.price_cents;
                any = true;
            }
        }
        return any ? sum : undefined;
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
                    {@const d = deviceOf(c)}
                    <li class="bat-card bat-detection">
                        <div class="bat-detection-head">
                            <DevicePicture model={c.model ?? d?.model ?? ''} src={d?.image} size={28} />
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
                            <button type="button" class="primary" disabled={busy !== 0} onclick={() => openConfirm(c)}>{t('inbox.confirm')}</button>
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
                {#if column.key === 'image'}
                    {@const d = deviceOf(row)}
                    <DevicePicture model={row.model ?? d?.model ?? ''} src={d?.image} size={20} />
                {:else if column.key === 'at'}
                    {formatDateTime(row.at, lang)}
                {:else if column.key === 'kind'}
                    <span class="bat-pill kind" title={t('kind.' + row.kind)}>{t('kind.' + row.kind)}</span>
                {:else if column.key === 'cost'}
                    <span class="bat-mono">{formatCents(cost(row), lang)}</span>
                {:else if column.key === 'actions'}
                    <button type="button" class="bat-cell-button" onclick={() => openEdit(row)} disabled={busy !== 0}>{t('Edit')}</button>
                    <button type="button" class="bat-cell-button" onclick={() => (confirmDelete = row)} disabled={busy !== 0}>{t('Delete')}</button>
                {:else}
                    {column.value ? String(column.value(row) ?? '') : String((row as unknown as Record<string, unknown>)[column.key] ?? '')}
                {/if}
            {/snippet}
        </DataTable>
    </div>
{/if}

<ChangeDialog bind:open={editorOpen} mode={editorMode} device={editorDevice(editing)} change={editing} at={editing?.at ?? Date.now()} busy={busy !== 0} onsubmit={save} />
<ChangeDeleteDialog change={confirmDelete} busy={busy !== 0} onclose={() => (confirmDelete = undefined)} onconfirm={(c) => (remove(c), (confirmDelete = undefined))} />
