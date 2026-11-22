<script lang="ts">
    // "Edit batteries": the device page's correction of the battery table - the power supply, the
    // battery slots (count and type), the chemistry, rechargeable cells and outdoor use, for this
    // device or for every device of its model, and "Reset" back to what the table says. It writes
    // the override the API keeps (devices/{iface}/{address}/override, models/{model}/override),
    // only what differs from the layer below, so a model the table lacks (a self-built device) is
    // made known the same way.
    import {untrack} from 'svelte';
    import {Dialog} from 'occulite-ui';
    import {t} from 'occulite-ui/i18n';

    import DevicePicture from './DevicePicture.svelte';
    import {del, get, put} from './api.js';
    import {baselineOf, chemistryLabel, EDITOR_POWERS, overrideOf, powerTakesBatteries, profileForm, profileFormValid, type Baseline, type DeviceView, type ModelInfo, type OverrideBody, type ProfileForm} from './model.js';

    export type Scope = 'device' | 'model';

    let {
        open = $bindable(false),
        device,
        deviceOverride,
        modelInfo,
        onsaved,
        onerror,
    }: {
        open?: boolean;
        device: DeviceView;
        deviceOverride: OverrideBody | null;
        modelInfo: ModelInfo | null;
        onsaved: (scope: Scope, removed: boolean) => void;
        onerror: (message: string) => void;
    } = $props();

    let scope = $state<Scope>('device');
    let form = $state<ProfileForm>({power: 'unknown', slots: [], chemistry: '', rechargeable: false, outdoor: false});
    let types = $state<string[]>([]);
    let chemistries = $state<string[]>([]);
    let busy = $state(false);

    // the layer below the chosen override: the table row for the model, the model's profile
    // (the row with the model's override) for the device
    function baseline(s: Scope): Baseline {
        if (s === 'model') return baselineOf(modelInfo?.row);
        return baselineOf(modelInfo?.profile ?? modelInfo?.row);
    }
    function overrideOfScope(s: Scope): OverrideBody | null {
        return (s === 'device' ? deviceOverride : modelInfo?.override) ?? null;
    }

    function fill(s: Scope) {
        form = profileForm(baseline(s), overrideOfScope(s), device.profile);
    }

    // filled when the dialog opens, on "this device only": a change for every device of the model
    // is a choice of its own
    $effect(() => {
        if (!open) return;
        untrack(() => {
            scope = 'device';
            fill(scope);
            if (types.length === 0) {
                get<{table: {types?: string[]; chemistries?: string[]}}>('api/v1/models')
                    .then((r) => {
                        types = r.table.types ?? [];
                        chemistries = r.table.chemistries ?? [];
                    })
                    .catch(() => {});
            }
        });
    });

    function setScope(s: Scope) {
        scope = s;
        fill(s);
    }

    function addSlot() {
        form.slots.push({count: 1, type: form.slots.at(-1)?.type ?? 'AA'});
    }

    const takesBatteries = $derived(powerTakesBatteries(form.power));
    // the type list holds the slots' own types even before the table's list arrived
    const typeOptions = $derived([...new Set([...types, ...form.slots.map((s) => s.type).filter(Boolean)])]);
    const valid = $derived(types.length === 0 || profileFormValid(form, types));
    const hasOverride = $derived(overrideOfScope(scope) !== null);
    const notInTable = $derived(!modelInfo?.in_table);

    async function save() {
        busy = true;
        const path = scope === 'device' ? `api/v1/devices/${encodeURIComponent(device.interface)}/${encodeURIComponent(device.address)}/override` : `api/v1/models/${encodeURIComponent(device.model)}/override`;
        try {
            const body = overrideOf(form, baseline(scope));
            if (body) await put(path, body);
            else if (hasOverride) await del(path);
            open = false;
            onsaved(scope, !body);
        } catch (e) {
            onerror((e as Error).message);
        } finally {
            busy = false;
        }
    }

    async function reset() {
        busy = true;
        const path = scope === 'device' ? `api/v1/devices/${encodeURIComponent(device.interface)}/${encodeURIComponent(device.address)}/override` : `api/v1/models/${encodeURIComponent(device.model)}/override`;
        try {
            await del(path);
            open = false;
            onsaved(scope, true);
        } catch (e) {
            onerror((e as Error).message);
        } finally {
            busy = false;
        }
    }
</script>

<Dialog bind:open title={t('editor.title')} width="34rem" closable={!busy} onclose={() => (open = false)} testId="battery-editor">
    <p class="bat-dialog-device">
        <DevicePicture model={device.model} src={device.image} size={28} />
        <span><strong>{device.name || device.address}</strong> <span class="bat-muted">{device.model} · <span class="bat-mono">{device.address}</span></span></span>
    </p>
    <fieldset class="bat-scope" data-testid="editor-scope">
        <legend>{t('editor.scope')}</legend>
        <label><input type="radio" name="bat-scope" value="device" checked={scope === 'device'} disabled={busy} onchange={() => setScope('device')} /> {t('editor.scope.device')}</label>
        <label><input type="radio" name="bat-scope" value="model" checked={scope === 'model'} disabled={busy} onchange={() => setScope('model')} /> {t('editor.scope.model', {model: device.model})}</label>
    </fieldset>
    <p class="bat-muted bat-editor-note" data-testid="editor-state">
        {#if hasOverride}{t(scope === 'device' ? 'editor.state.device' : 'editor.state.model')}{:else if notInTable}{t('editor.state.notInTable')}{:else}{t('editor.state.table')}{/if}
        {#if scope === 'model' && deviceOverride}{' '}{t('editor.deviceWins')}{/if}
    </p>
    <div class="bat-form">
        <label for="bat-ed-power">{t('supply.label')}</label>
        <select id="bat-ed-power" data-testid="editor-power" bind:value={form.power} disabled={busy}>
            {#if form.power === 'unknown'}<option value="unknown">{t('power.unknown')}</option>{/if}
            {#each EDITOR_POWERS as p (p)}<option value={p}>{t('power.' + p)}</option>{/each}
        </select>
    </div>
    {#if takesBatteries}
        <fieldset class="bat-slot" data-testid="editor-slots">
            <legend>{t('editor.slots')}</legend>
            {#each form.slots as s, i (i)}
                <div class="bat-slot-row">
                    <input class="hmm-input bat-count-input" type="number" min="1" max="12" step="1" aria-label={t('change.count')} data-testid="editor-count-{i}" bind:value={s.count} disabled={busy} />
                    <span aria-hidden="true">×</span>
                    <select aria-label={t('change.type')} data-testid="editor-type-{i}" value={s.type} disabled={busy} onchange={(e) => ((s.type = e.currentTarget.value), delete s.chemistry)}>
                        {#each typeOptions as ty (ty)}<option value={ty}>{ty}</option>{/each}
                    </select>
                    <button type="button" class="bat-cell-button" data-testid="editor-remove-{i}" title={t('editor.slot.remove')} aria-label={t('editor.slot.remove')} disabled={busy} onclick={() => form.slots.splice(i, 1)}>✕</button>
                </div>
            {/each}
            {#if form.slots.length === 0}<p class="bat-muted">{t('editor.slots.none')}</p>{/if}
            <button type="button" class="bat-cell-button bat-add-slot" data-testid="editor-add-slot" disabled={busy || form.slots.length >= 4} onclick={addSlot}>{t('editor.slot.add')}</button>
        </fieldset>
        <div class="bat-form">
            <label for="bat-ed-chem">{t('editor.chemistry')}</label>
            <select id="bat-ed-chem" data-testid="editor-chemistry" bind:value={form.chemistry} disabled={busy}>
                <option value="">{t('editor.chemistry.type')}</option>
                {#each chemistries as c (c)}<option value={c}>{chemistryLabel(c, t)}</option>{/each}
                {#if form.chemistry && !chemistries.includes(form.chemistry)}<option value={form.chemistry}>{form.chemistry}</option>{/if}
            </select>
            <label for="bat-ed-rech">{t('change.rechargeable')}</label>
            <span><input id="bat-ed-rech" type="checkbox" data-testid="editor-rechargeable" bind:checked={form.rechargeable} disabled={busy} /> <span class="bat-muted">{t('editor.rechargeable.hint')}</span></span>
        </div>
    {/if}
    <div class="bat-form">
        <label for="bat-ed-outdoor">{t('editor.outdoor')}</label>
        <span><input id="bat-ed-outdoor" type="checkbox" data-testid="editor-outdoor" bind:checked={form.outdoor} disabled={busy} /> <span class="bat-muted">{t('editor.outdoor.hint')}</span></span>
    </div>
    {#snippet buttons()}
        <div class="bat-dialog-buttons">
            <button type="button" class="bat-reset" data-testid="editor-reset" title={t('editor.reset.title')} disabled={busy || !hasOverride} onclick={reset}>{t('editor.reset')}</button>
            <button type="button" onclick={() => (open = false)} disabled={busy}>{t('Cancel')}</button>
            <button type="button" class="primary" data-testid="editor-save" disabled={busy || !valid} onclick={save}>{t('editor.save')}</button>
        </div>
    {/snippet}
</Dialog>
