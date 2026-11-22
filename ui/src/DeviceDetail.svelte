<script lang="ts">
    // One device: its picture and profile, how long its batteries lasted, the voltage curve over
    // the whole record with the changes marked, and the recorded changes with their batteries -
    // editable, deletable, and "Changed..." for a new one.
    import {i18n, t} from 'occulite-ui/i18n';
    import type {NoticesStore} from 'occulite-ui/notices';

    import BatteryEditor, {type Scope} from './BatteryEditor.svelte';
    import ChangeDeleteDialog from './ChangeDeleteDialog.svelte';
    import ChangeDialog, {type EditorResult} from './ChangeDialog.svelte';
    import DevicePicture from './DevicePicture.svelte';
    import VoltageChart, {type SamplePoint} from './VoltageChart.svelte';
    import {ApiError, del, get, patch, post, put} from './api.js';
    import {batteryName, chemistryLabel, describeSlots, originsOf, type ModelInfo, type OverrideBody, type Origin, formatCents, formatDateTime, formatDays, formatVoltage, lifeText, presetText, relativeTime, statusOf, type Change, type DeviceView} from './model.js';

    let {device, notices, onback, onchanged}: {device: DeviceView; notices: NoticesStore; onback: () => void; onchanged?: () => void} = $props();

    let samples = $state<SamplePoint[]>([]);
    let changes = $state<Change[]>([]);
    let loading = $state(true);
    let error = $state('');
    let busy = $state(false);
    let editorOpen = $state(false);
    let editorMode = $state<'record' | 'edit'>('record');
    let editing = $state<Change | undefined>();
    // the notes shown in full (a long one is clamped to two lines; a click opens it)
    let openNotes = $state<Record<number, boolean>>({});

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

    // the device's own override (GET devices/{iface}/{address}/override) and its model's (GET
    // models/{model}): the battery editor writes either, the power supply switch of a model that
    // runs on batteries or a power supply unit writes the device's power. This device wins over
    // its model, the model over the table.
    type Override = OverrideBody;
    let override = $state<Override | null>(null);
    let modelInfo = $state<ModelInfo | null>(null);
    let editorBatteriesOpen = $state(false);
    const devPath = $derived(`api/v1/devices/${encodeURIComponent(device.interface)}/${encodeURIComponent(device.address)}`);

    async function loadOverride() {
        try {
            override = (await get<{override: Override | null}>(`${devPath}/override`)).override;
        } catch {
            override = null;
        }
        try {
            modelInfo = await get<ModelInfo>(`api/v1/models/${encodeURIComponent(device.model)}`);
        } catch (e) {
            // 404: the table lacks the model and nobody set it
            modelInfo = e instanceof ApiError && e.status === 404 ? {in_table: false} : null;
        }
    }

    $effect(() => {
        void key;
        loadOverride();
    });

    const origins = $derived(originsOf(device.profile, override, modelInfo?.override, !!modelInfo?.in_table));
    // the slots' source: a preset (the table stand) by its own label and the level that set it
    const presetLabel = $derived(device.profile?.preset ? presetText(modelInfo?.presets?.[device.profile.preset], 'label', lang) : '');
    function slotsOriginText(o: Origin): string {
        if (presetLabel && (o === 'device' || o === 'model')) return t('origin.preset.' + o, {label: presetLabel, model: device.model});
        return o === 'auto' || o === 'none' ? (device.profile ? t('profile.' + device.profile.source) : '') : originText(o);
    }
    function originText(o: Origin): string {
        return o === 'model' ? t('origin.model', {model: device.model}) : t('origin.' + o);
    }
    const chemistry = $derived(device.profile?.slots?.find((s) => s.chemistry)?.chemistry ?? '');

    async function editorSaved(scope: Scope, removed: boolean) {
        notices.push('info', t(`editor.${removed ? 'removed' : 'saved'}.${scope}`, {model: device.model}));
        await loadOverride();
        onchanged?.();
    }

    async function resetLevel(scope: Scope) {
        busy = true;
        try {
            await del(scope === 'device' ? `${devPath}/override` : `api/v1/models/${encodeURIComponent(device.model)}/override`);
            await editorSaved(scope, true);
        } catch (e) {
            notices.push('error', t('editor.failed', {error: (e as Error).message}));
        } finally {
            busy = false;
        }
    }

    // the power supply choice: automatic (no power in the override: the addon tells it by the
    // voltage), batteries or the power supply unit (the override's power, which wins)
    type Supply = 'auto' | 'batteries' | 'external';
    const supply = $derived<Supply>(override?.power === 'external' ? 'external' : override?.power === 'battery' ? 'batteries' : 'auto');
    // the switch is offered where the model can run either way (the table's battery-or-dc), and
    // wherever the user set it to the supply already
    const supplyChoice = $derived(device.profile?.table_power === 'battery-or-dc' || device.profile?.power === 'battery-or-dc' || device.profile?.auto || supply === 'external');
    // what the automatic choice found: on the supply (the profile went external by the voltage)
    const autoOnSupply = $derived(!!device.profile?.auto);

    async function setSupply(choice: Supply) {
        busy = true;
        try {
            const next: Override = {...(override ?? {})};
            if (choice === 'external') next.power = 'external';
            else if (choice === 'batteries') next.power = 'battery';
            else delete next.power;
            if (Object.keys(next).length === 0) await del(`${devPath}/override`);
            else await put(`${devPath}/override`, next);
            override = Object.keys(next).length ? next : null;
            notices.push('info', t('supply.saved'));
            onchanged?.();
        } catch (e) {
            notices.push('error', t('ignore.failed', {error: (e as Error).message}));
        } finally {
            busy = false;
        }
    }

    async function setIgnored(on: boolean) {
        busy = true;
        try {
            if (on) await put(`${devPath}/ignore`, undefined);
            else await del(`${devPath}/ignore`);
            notices.push('info', t(on ? 'ignore.done' : 'ignore.undone', {name: device.name || device.address}));
            onchanged?.();
        } catch (e) {
            notices.push('error', t('ignore.failed', {error: (e as Error).message}));
        } finally {
            busy = false;
        }
    }

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
            <dd data-testid="slots">{describeSlots(device.profile?.slots) || t('profile.unknown')} <span class="bat-muted" data-testid="slots-origin">({slotsOriginText(origins.slots)})</span></dd>
            {#if !supplyChoice}
                <dt>{t('supply.label')}</dt>
                <dd data-testid="power">{t('power.' + (device.profile?.power ?? 'unknown'))} <span class="bat-muted" data-testid="power-origin">({originText(origins.power)})</span></dd>
            {/if}
            {#if chemistry}
                <dt>{t('editor.chemistry')}</dt>
                <dd data-testid="chemistry">{chemistryLabel(chemistry, t)} <span class="bat-muted" data-testid="chemistry-origin">({originText(origins.chemistry)})</span></dd>
            {/if}
            {#if device.profile?.outdoor}
                <dt>{t('editor.outdoor')}</dt>
                <dd data-testid="outdoor">✓ <span class="bat-muted" data-testid="outdoor-origin">({originText(origins.outdoor)})</span></dd>
            {/if}
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
            {#if supplyChoice}
                <dt>{t('supply.label')}</dt>
                <dd>
                    <select data-testid="supply" title={t('supply.title')} value={supply} disabled={busy} onchange={(e) => setSupply(e.currentTarget.value as Supply)}>
                        <option value="auto">{t(autoOnSupply ? 'supply.auto.external' : 'supply.auto.batteries')}</option>
                        <option value="batteries">{t('supply.batteries')}</option>
                        <option value="external">{t('supply.external')}</option>
                    </select>
                    {#if supply === 'auto' && autoOnSupply}
                        <span class="bat-muted" data-testid="supply-auto">{t('supply.detected')}</span>
                    {/if}
                </dd>
            {/if}
            {#if override || modelInfo?.override}
                <dt>{t('own.label')}</dt>
                <dd class="bat-own" data-testid="own-values">
                    {#if modelInfo?.override}
                        <span class="bat-own-level" data-testid="own-model">{t('own.model', {model: device.model})} <button type="button" class="bat-cell-button" data-testid="reset-model" title={t('editor.reset.title')} disabled={busy} onclick={() => resetLevel('model')}>{t('editor.reset')}</button></span>
                    {/if}
                    {#if override}
                        <span class="bat-own-level" data-testid="own-device">{t('own.device')} <button type="button" class="bat-cell-button" data-testid="reset-device" title={t('editor.reset.title')} disabled={busy} onclick={() => resetLevel('device')}>{t('editor.reset')}</button></span>
                    {/if}
                </dd>
            {/if}
            {#if device.ignored}
                <dt></dt>
                <dd class="bat-muted" data-testid="ignored-note">{t('ignore.detail')}</dd>
            {/if}
        </dl>
        <div class="bat-actions">
            <button type="button" class="primary" onclick={openRecord} disabled={busy}>{t('change.record.button')}</button>
            <button type="button" data-testid="edit-batteries" title={t('editor.button.title')} onclick={() => (editorBatteriesOpen = true)} disabled={busy}>{t('editor.button')}</button>
            <button type="button" data-testid="ignore-toggle" title={device.ignored ? t('ignore.undo') : t('ignore.title')} onclick={() => setIgnored(!device.ignored)} disabled={busy}
                >{device.ignored ? t('ignore.undo') : t('ignore.button')}</button
            >
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
                <div class="bat-table-scroll">
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
                                    <td class="bat-note-cell">
                                        {#if c.note}
                                            <button type="button" class="bat-note" title={c.note} aria-expanded={!!openNotes[c.id]} onclick={() => (openNotes[c.id] = !openNotes[c.id])}>{c.note}</button>
                                        {/if}
                                    </td>
                                    <td class="bat-row-actions">
                                        <button type="button" class="bat-cell-button" onclick={() => openEdit(c)} disabled={busy}>{t('Edit')}</button>
                                        <button type="button" class="bat-cell-button" onclick={() => (confirmDelete = c)} disabled={busy}>{t('Delete')}</button>
                                    </td>
                                </tr>
                            {/each}
                        </tbody>
                    </table>
                </div>
            {/if}
        </section>
    {/if}
</div>

<ChangeDialog bind:open={editorOpen} mode={editorMode} {device} change={editing} at={Date.now()} {busy} onsubmit={save} />

<BatteryEditor bind:open={editorBatteriesOpen} {device} deviceOverride={override} {modelInfo} onsaved={editorSaved} onerror={(m) => notices.push('error', t('editor.failed', {error: m}))} />

<ChangeDeleteDialog change={confirmDelete} {busy} onclose={() => (confirmDelete = undefined)} onconfirm={(c) => (remove(c), (confirmDelete = undefined))} />
