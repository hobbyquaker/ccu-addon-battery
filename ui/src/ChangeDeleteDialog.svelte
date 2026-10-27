<script lang="ts">
    // "Delete this change?" - the kit's dialog, never window.confirm.
    import {Dialog} from 'occulite-ui';
    import {i18n, t} from 'occulite-ui/i18n';

    import {formatDateTime, type Change} from './model.js';

    let {change, busy = false, onclose, onconfirm}: {change: Change | undefined; busy?: boolean; onclose: () => void; onconfirm: (c: Change) => void} = $props();
    const open = $derived(change !== undefined);
</script>

<Dialog {open} title={t('change.delete.title')} width="26rem" closable={!busy} {onclose} testId="change-delete">
    {#if change}
        <p>{t('change.delete.question', {name: change.name || change.address, when: formatDateTime(change.at, i18n.language)})}</p>
    {/if}
    {#snippet buttons()}
        <div class="bat-dialog-buttons">
            <button type="button" onclick={onclose} disabled={busy}>{t('Cancel')}</button>
            <button type="button" class="primary danger" disabled={busy || !change} onclick={() => change && onconfirm(change)}>{t('Delete')}</button>
        </div>
    {/snippet}
</Dialog>
