<script lang="ts">
    // One dialog for two questions: "I changed the batteries" for a device (a new manual change,
    // the slots prefilled from its profile) and "it was another day" for an inbox entry (the date
    // of a detection, confirmed with it).
    import {Dialog} from 'occulite-ui';
    import {t} from 'occulite-ui/i18n';

    import {get} from './api.js';
    import {describeSlots, fromLocalInput, toLocalInput, type DeviceView} from './model.js';

    let {
        open = $bindable(false),
        device,
        at,
        mode,
        busy = false,
        onsubmit,
    }: {
        open?: boolean;
        device: Pick<DeviceView, 'name' | 'address' | 'model' | 'profile'> | undefined;
        at: number;
        mode: 'record' | 'redate';
        busy?: boolean;
        onsubmit: (r: {at: number; note: string; vendor: string}) => void;
    } = $props();

    let value = $state('');
    let note = $state('');
    let vendor = $state('');
    let vendors = $state<string[]>([]);

    $effect(() => {
        if (open) {
            value = toLocalInput(at);
            note = '';
            vendor = '';
            if (mode === 'record') {
                // the vendors recorded so far, so one is not written three ways
                get<{suggestions: {value: string}[]}>('api/v1/suggestions?field=vendor&limit=50')
                    .then((r) => (vendors = r.suggestions.map((s) => s.value)))
                    .catch(() => (vendors = []));
            }
        }
    });

    const ms = $derived(fromLocalInput(value));
    const valid = $derived(Number.isFinite(ms) && ms <= Date.now() + 86_400_000);
</script>

<Dialog bind:open title={mode === 'record' ? t('change.record.title') : t('change.redate.title')} width="28rem" closable={!busy} onclose={() => (open = false)} testId="change-dialog">
    {#if device}
        <p><strong>{device.name || device.address}</strong> <span class="bat-muted">{device.model} · <span class="bat-mono">{device.address}</span></span></p>
        {#if mode === 'record' && describeSlots(device.profile?.slots)}
            <p class="bat-muted">{t('change.record.takes', {slots: describeSlots(device.profile?.slots)})}</p>
        {/if}
    {/if}
    <div class="bat-form">
        <label for="bat-change-at">{mode === 'record' ? t('change.record.when') : t('change.redate.when')}</label>
        <input id="bat-change-at" type="datetime-local" bind:value />
        {#if mode === 'record'}
            <label for="bat-change-vendor">{t('change.vendor')}</label>
            <input id="bat-change-vendor" type="text" bind:value={vendor} maxlength="80" list="bat-vendors" autocomplete="off" />
            <datalist id="bat-vendors">
                {#each vendors as v (v)}<option value={v}></option>{/each}
            </datalist>
            <label for="bat-change-note">{t('change.note')}</label>
            <input id="bat-change-note" type="text" bind:value={note} maxlength="200" />
        {/if}
    </div>
    {#snippet buttons()}
        <div class="bat-dialog-buttons">
            <button type="button" onclick={() => (open = false)} disabled={busy}>{t('Cancel')}</button>
            <button type="button" class="primary" disabled={busy || !valid} onclick={() => onsubmit({at: ms, note, vendor})}>
                {mode === 'record' ? t('change.record.save') : t('inbox.confirm')}
            </button>
        </div>
    {/snippet}
</Dialog>
