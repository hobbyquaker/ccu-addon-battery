<script lang="ts">
    // The change editor, one dialog for three questions: "I changed the batteries" of a device
    // (a new manual change), "yes, changed" for an inbox entry (confirmed with the batteries and,
    // if need be, another date) and the edit of a recorded change. Per slot: the size and count
    // from the profile, the vendor and the product line (with the values recorded so far as
    // suggestions, the product lines narrowed to the vendor), rechargeable, and the price of one
    // battery - prefilled with the last price recorded for that vendor and product line.
    import {Dialog} from 'occulite-ui';
    import {i18n, t} from 'occulite-ui/i18n';

    import DevicePicture from './DevicePicture.svelte';
    import {get} from './api.js';
    import {describeSlots, fromLocalInput, parsePrice, priceInput, toLocalInput, type Battery, type Change, type DeviceView} from './model.js';

    export interface EditorResult {
        at: number;
        note: string;
        batteries: Battery[];
    }

    let {
        open = $bindable(false),
        device,
        change = undefined,
        at,
        mode,
        busy = false,
        onsubmit,
    }: {
        open?: boolean;
        device: (Pick<DeviceView, 'name' | 'address' | 'model' | 'profile'> & {image?: string}) | undefined;
        /** The change being confirmed or edited; its batteries prefill the slots. */
        change?: Pick<Change, 'at' | 'note' | 'batteries'> | undefined;
        at: number;
        mode: 'record' | 'confirm' | 'edit';
        busy?: boolean;
        onsubmit: (r: EditorResult) => void;
    } = $props();

    interface SlotEdit {
        count: number;
        type: string;
        chemistry: string;
        vendor: string;
        product: string;
        rechargeable: boolean;
        price: string;
        /** The price came from the prefill, not the user: a new vendor/product may replace it. */
        prefilled: boolean;
    }

    let value = $state('');
    let note = $state('');
    let slots = $state<SlotEdit[]>([]);
    let vendors = $state<string[]>([]);
    let productsByVendor = $state<Record<string, string[]>>({});

    const lang = $derived(i18n.language);

    function initialSlots(): SlotEdit[] {
        const fromChange = change?.batteries ?? [];
        const base: Battery[] =
            fromChange.length > 0 ? fromChange : (device?.profile?.slots ?? []).map((s, i) => ({slot: i + 1, count: s.count, type: s.type, chemistry: s.chemistry}));
        if (base.length === 0) base.push({slot: 1, count: 1, type: ''});
        return base.map((b) => ({
            count: b.count ?? 1,
            type: b.type ?? '',
            chemistry: b.chemistry ?? '',
            vendor: b.vendor ?? '',
            product: b.product ?? '',
            rechargeable: b.rechargeable ?? false,
            price: priceInput(b.price_cents, i18n.language),
            prefilled: false,
        }));
    }

    $effect(() => {
        if (!open) return;
        value = toLocalInput(change?.at ?? at);
        note = change?.note ?? '';
        slots = initialSlots();
        get<{suggestions: {value: string}[]}>('api/v1/suggestions?field=vendor&limit=50')
            .then((r) => (vendors = r.suggestions.map((s) => s.value)))
            .catch(() => (vendors = []));
        for (const s of slots) {
            if (s.vendor) void loadProducts(s.vendor);
            if (!s.price && (s.vendor || s.product)) void prefillPrice(s);
        }
    });

    async function loadProducts(vendor: string): Promise<void> {
        const key = vendor.trim().toLowerCase();
        if (!key || productsByVendor[key]) return;
        try {
            const r = await get<{suggestions: {value: string}[]}>(`api/v1/suggestions?field=product&limit=50&vendor=${encodeURIComponent(vendor.trim())}`);
            productsByVendor = {...productsByVendor, [key]: r.suggestions.map((s) => s.value)};
        } catch {
            productsByVendor = {...productsByVendor, [key]: []};
        }
    }

    async function prefillPrice(s: SlotEdit): Promise<void> {
        if (s.price && !s.prefilled) return;
        const q = new URLSearchParams();
        if (s.vendor.trim()) q.set('vendor', s.vendor.trim());
        if (s.product.trim()) q.set('product', s.product.trim());
        if (s.type.trim()) q.set('type', s.type.trim());
        try {
            const r = await get<{found: boolean; price_cents?: number}>(`api/v1/prices/last?${q}`);
            if (r.found && r.price_cents !== undefined && (!s.price || s.prefilled)) {
                s.price = priceInput(r.price_cents, lang);
                s.prefilled = true;
            }
        } catch {
            /* no prefill */
        }
    }

    function onVendor(s: SlotEdit): void {
        void loadProducts(s.vendor);
        void prefillPrice(s);
    }

    const ms = $derived(fromLocalInput(value));
    const prices = $derived(slots.map((s) => parsePrice(s.price)));
    const priceOk = $derived(prices.every((p) => p === undefined || !Number.isNaN(p)));
    const valid = $derived(Number.isFinite(ms) && ms <= Date.now() + 86_400_000 && priceOk && slots.every((s) => s.count >= 1 && s.count <= 12));
    const title = $derived(mode === 'record' ? t('change.record.title') : mode === 'confirm' ? t('change.confirm.title') : t('change.edit.title'));
    const total = $derived(slots.reduce((sum, s, i) => sum + (prices[i] && !Number.isNaN(prices[i]) ? s.count * (prices[i] as number) : 0), 0));

    function submit(): void {
        const batteries: Battery[] = slots.map((s, i) => {
            const b: Battery = {slot: i + 1, count: s.count, type: s.type.trim() || undefined};
            if (s.chemistry) b.chemistry = s.chemistry;
            if (s.vendor.trim()) b.vendor = s.vendor.trim();
            if (s.product.trim()) b.product = s.product.trim();
            if (s.rechargeable) b.rechargeable = true;
            const p = prices[i];
            if (p !== undefined && !Number.isNaN(p)) b.price_cents = p;
            return b;
        });
        onsubmit({at: ms, note, batteries});
    }
</script>

<Dialog bind:open {title} width="34rem" closable={!busy} onclose={() => (open = false)} testId="change-dialog">
    {#if device}
        <p class="bat-dialog-device">
            <DevicePicture model={device.model} src={device.image} size={28} />
            <span><strong>{device.name || device.address}</strong> <span class="bat-muted">{device.model} · <span class="bat-mono">{device.address}</span></span></span>
        </p>
        {#if describeSlots(device.profile?.slots)}
            <p class="bat-muted">{t('change.record.takes', {slots: describeSlots(device.profile?.slots)})}</p>
        {/if}
    {/if}
    <div class="bat-form">
        <label for="bat-change-at">{mode === 'record' ? t('change.record.when') : t('change.redate.when')}</label>
        <input id="bat-change-at" class="hmm-input" type="datetime-local" bind:value />
    </div>
    {#each slots as s, i (i)}
        <fieldset class="bat-slot">
            <legend>
                {#if slots.length > 1}{t('change.slot', {n: i + 1})}: {/if}
                {#if s.type}{s.count}× {s.type}{:else}{t('change.slot.unknownType')}{/if}
            </legend>
            <div class="bat-form">
                {#if !s.type}
                    <label for="bat-type-{i}">{t('change.type')}</label>
                    <input id="bat-type-{i}" class="hmm-input" type="text" bind:value={s.type} maxlength="20" onchange={() => prefillPrice(s)} />
                    <label for="bat-count-{i}">{t('change.count')}</label>
                    <input id="bat-count-{i}" class="hmm-input" type="number" min="1" max="12" bind:value={s.count} />
                {/if}
                <label for="bat-vendor-{i}">{t('change.vendor')}</label>
                <input id="bat-vendor-{i}" class="hmm-input" type="text" bind:value={s.vendor} maxlength="80" list="bat-vendors" autocomplete="off" placeholder={t('change.vendor.example')} onchange={() => onVendor(s)} />
                <label for="bat-product-{i}">{t('change.product')}</label>
                <input id="bat-product-{i}" class="hmm-input" type="text" bind:value={s.product} maxlength="80" list="bat-products-{i}" autocomplete="off" placeholder={t('change.product.example')} onchange={() => prefillPrice(s)} />
                <datalist id="bat-products-{i}">
                    {#each productsByVendor[s.vendor.trim().toLowerCase()] ?? [] as p (p)}<option value={p}></option>{/each}
                </datalist>
                <label for="bat-price-{i}">{t('change.price')}</label>
                <span class="bat-price-field">
                    <input id="bat-price-{i}" class="hmm-input bat-price-input" type="text" inputmode="decimal" bind:value={s.price} maxlength="10" placeholder={lang === 'de' ? '0,89' : '0.89'} oninput={() => (s.prefilled = false)} aria-invalid={prices[i] !== undefined && Number.isNaN(prices[i])} />
                    <span class="bat-muted">€ {t('change.price.each')}{#if s.prefilled} · {t('change.price.prefilled')}{/if}</span>
                </span>
                <label for="bat-rech-{i}">{t('change.rechargeable')}</label>
                <span><input id="bat-rech-{i}" type="checkbox" bind:checked={s.rechargeable} /></span>
            </div>
        </fieldset>
    {/each}
    <datalist id="bat-vendors">
        {#each vendors as v (v)}<option value={v}></option>{/each}
    </datalist>
    <div class="bat-form">
        <label for="bat-change-note">{t('change.note')}</label>
        <input id="bat-change-note" class="hmm-input" type="text" bind:value={note} maxlength="200" />
    </div>
    <p class="bat-muted bat-total">{total > 0 ? t('change.total', {total: (total / 100).toLocaleString(lang, {style: 'currency', currency: 'EUR'})}) : ' '}</p>
    {#snippet buttons()}
        <div class="bat-dialog-buttons">
            <button type="button" onclick={() => (open = false)} disabled={busy}>{t('Cancel')}</button>
            <button type="button" class="primary" disabled={busy || !valid} onclick={submit}>
                {mode === 'record' ? t('change.record.save') : mode === 'confirm' ? t('inbox.confirm') : t('change.edit.save')}
            </button>
        </div>
    {/snippet}
</Dialog>
