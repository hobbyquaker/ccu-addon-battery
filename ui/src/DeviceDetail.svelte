<script lang="ts">
    // One device: its picture and profile, how long its batteries lasted, the voltage curve over
    // the whole record with the changes marked, and the recorded changes with their batteries -
    // editable, deletable, and "Changed..." for a new one.
    import {i18n, t} from 'occulite-ui/i18n';
    import type {NoticesStore} from 'occulite-ui/notices';

    import ChangeDeleteDialog from './ChangeDeleteDialog.svelte';
    import ChangeDialog, {type EditorResult} from './ChangeDialog.svelte';
    import DevicePicture from './DevicePicture.svelte';
    import VoltageChart, {type SamplePoint} from './VoltageChart.svelte';
    import {del, get, patch, post} from './api.js';
    import {batteryName, describeSlots, formatCents, formatDateTime, formatDays, formatVoltage, lifeText, relativeTime, statusOf, type Change, type DeviceView} from './model.js';

    let {device, notices, onback, onchanged}: {device: DeviceView; notices: NoticesStore; onback: () => void; onchanged?: () => void} = $props();

    let samples = $state<SamplePoint[]>([]);
    let changes = $state<Change[]>([]);
    let loading = $state(true);
    let error = $state('');
    let busy = $state(false);
    let editorOpen = $state(false);
    let editorMode = $state<'record' | 'edit'>('record');
    let editing = $state<Change | undefined>();

    const lang = $derived(i18n.language);
    const key = $derived(`${device.interface}.${device.address}`);

    async function load() {
        try {
            const q = `device=${encodeURIComponent(device.address)}&interface=${encodeURIComponent(device.interface)}`;
            const [s, c] = await Promise.all([
                get<{samples: SamplePoint[]}>(`api/v1/samples?${q}`),
                get<{changes: Change[]}>(`api/v1/changes?${q}&status=accepted,pending&limit=500&lang=${i18n.language}`),
            ]);
            samples = s.samples;
            changes = c.changes;
            error = '';
        } catch (e) {
            error = (e as Error).message;
        } finally {
            loading = false;
        }
    }

    $effect(() => {
        void key;
        void i18n.language;
        loading = true;
        load();
    });

    function openRecord() {
        editing = undefined;
        editorMode = 'record';
        editorOpen = true;
    }

    function openEdit(c: Change) {
        editing = c;
        editorMode = 'edit';
        editorOpen = true;
    }

    async function save(r: EditorResult) {
        busy = true;
        try {
            if (editorMode === 'edit' && editing) {
                await patch(`api/v1/changes/${editing.id}`, {at: r.at, note: r.note, batteries: r.batteries});
                notices.push('info', t('change.edit.saved'));
            } else {
                const out = await post<{status: string}>('api/v1/changes', {device: device.address, interface: device.interface, at: r.at, note: r.note, batteries: r.batteries, source: 'manual'});
                notices.push('info', t('change.record.' + out.status, {name: device.name || device.address}));
            }
            editorOpen = false;
            await load();
            onchanged?.();
        } catch (e) {
            notices.push('error', t('change.record.failed', {error: (e as Error).message}));
        } finally {
            busy = false;
        }
    }

    async function remove(c: Change) {
        busy = true;
        try {
            await del(`api/v1/changes/${c.id}`);
            notices.push('info', t('change.deleted'));
            await load();
            onchanged?.();
        } catch (e) {
            notices.push('error', t('change.record.failed', {error: (e as Error).message}));
        } finally {
            busy = false;
        }
    }

    let confirmDelete = $state<Change | undefined>();

    const accepted = $derived(changes.filter((c) => c.status === 'accepted').sort((a, b) => b.at - a.at));
    const status = $derived(statusOf(device));

    function slotCost(c: Change): number | undefined {
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
</script>

<div class="bat-detail" data-testid="device-detail">
    <div class="bat-detail-head">
        <button type="button" class="bat-link" onclick={onback}>‹ {t('Devices')}</button>
    </div>
    <section class="bat-card bat-detail-card">
        <div class="bat-detail-title">
            <DevicePicture model={device.model} src={device.image} size={48} />
            <div>
                <h2>{device.name || device.address}</h2>
                <div class="bat-muted">{device.model} · <span class="bat-mono">{device.address}</span>{#if device.rooms.length} · {device.rooms.join(', ')}{/if}</div>
            </div>
            <span class="bat-spacer"></span>
            <span class="bat-pill {status}" title={t('status.' + status)}>{t('status.' + status)}</span>
        </div>
        <dl class="bat-kv">
            <dt>{t('Batteries')}</dt>
            <dd>{describeSlots(device.profile?.slots) || t('profile.unknown')} <span class="bat-muted">({device.profile ? t('profile.' + device.profile.source) : ''})</span></dd>
            <dt>{t('Voltage')}</dt>
            <dd class="bat-mono">{formatVoltage(device.voltage, lang) || '–'}{#if device.voltage_ts}{' '}<span class="bat-muted">{relativeTime(device.voltage_ts, Date.now(), lang)}</span>{/if}</dd>
            {#if device.profile?.fresh}
                <dt>{t('detail.levels')}</dt>
                <dd class="bat-mono">{formatVoltage(device.profile.fresh, lang)} / {formatVoltage(device.profile.low, lang)}</dd>
            {/if}
            <dt>{t('life.column')}</dt>
            <dd><span class="bat-life" class:short={device.life?.hint === 'short'}>{lifeText(device.life, lang) || t('life.none')}</span></dd>
            {#if device.life?.running_days !== undefined}
                <dt>{t('life.running')}</dt>
                <dd>{t('life.runningSince', {days: formatDays(device.life.running_days, lang)})}{#if device.life.hint === 'short'} <span class="bat-life short">{t('life.hint.short')}</span>{/if}</dd>
            {/if}
            {#if !device.voltage_dp}
                <dt></dt>
                <dd class="bat-muted">{t('detail.noVoltageModel')}</dd>
            {/if}
        </dl>
        <div class="bat-actions">
            <button type="button" class="primary" onclick={openRecord} disabled={busy}>{t('change.record.button')}</button>
        </div>
    </section>

    {#if error}
        <div class="bat-banner" role="alert">{t('Could not load: {error}', {error})}</div>
    {:else if loading}
        <p class="bat-empty">{t('Loading…')}</p>
    {:else}
        <section class="bat-card bat-detail-card">
            <h2>{t('detail.curve')}</h2>
            <VoltageChart {samples} {changes} fresh={device.profile?.fresh} low={device.profile?.low} />
        </section>
        <section class="bat-card bat-detail-card">
            <h2>{t('changes.title')} <span class="bat-muted">({accepted.length})</span></h2>
            {#if accepted.length === 0}
                <p class="bat-empty">{t('changes.empty')}</p>
            {:else}
                <table class="bat-plain">
                    <thead>
                        <tr><th>{t('change.at')}</th><th>{t('Batteries')}</th><th>{t('change.cost')}</th><th>{t('change.kind')}</th><th>{t('change.note')}</th><th></th></tr>
                    </thead>
                    <tbody>
                        {#each accepted as c (c.id)}
                            <tr>
                                <td>{formatDateTime(c.at, lang)}</td>
                                <td>{c.batteries.map((b) => `${b.count ?? 1}× ${batteryName(b, t('battery.unnamed'))}${b.rechargeable ? ' ⟳' : ''}`).join(' + ') || '–'}</td>
                                <td class="bat-mono">{formatCents(slotCost(c), lang) || '–'}</td>
                                <td><span class="bat-pill kind" title={t('kind.' + c.kind)}>{t('kind.' + c.kind)}</span></td>
                                <td class="bat-truncate">{c.note}</td>
                                <td class="bat-row-actions">
                                    <button type="button" class="bat-cell-button" onclick={() => openEdit(c)} disabled={busy}>{t('Edit')}</button>
                                    <button type="button" class="bat-cell-button" onclick={() => (confirmDelete = c)} disabled={busy}>{t('Delete')}</button>
                                </td>
                            </tr>
                        {/each}
                    </tbody>
                </table>
            {/if}
        </section>
    {/if}
</div>

<ChangeDialog bind:open={editorOpen} mode={editorMode} {device} change={editing} at={Date.now()} {busy} onsubmit={save} />

<ChangeDeleteDialog change={confirmDelete} {busy} onclose={() => (confirmDelete = undefined)} onconfirm={(c) => (remove(c), (confirmDelete = undefined))} />
