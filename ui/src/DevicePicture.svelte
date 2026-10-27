<script lang="ts">
    // The platform's picture of a device model, the way Homematic Manager shows it: a fixed box
    // so rows never jump, lazy loading, the dark-mode inversion from the theme's token, and a
    // neutral placeholder with two letters of the model where there is no picture.
    import {modelInitials} from './model.js';

    let {
        model = '',
        src = undefined,
        size = 20,
        title = undefined,
    }: {
        model?: string;
        /** The URL path from the API (`/config/img/devices/250/…`); undefined without one. */
        src?: string | undefined;
        size?: number;
        title?: string | undefined;
    } = $props();

    let broken = $state(false);
    $effect(() => {
        void src;
        broken = false;
    });
    const show = $derived(src !== undefined && src !== '' && !broken);
</script>

{#if show}
    <img class="bat-devimg" {src} alt={model} title={title ?? model} width={size} height={size} loading="lazy" decoding="async" onerror={() => (broken = true)} />
{:else}
    <span class="bat-devimg bat-devimg-fallback" title={title ?? model} aria-label={model || undefined} role={model ? 'img' : undefined} style:width={`${size}px`} style:height={`${size}px`} style:font-size={`${Math.max(6, Math.round(size * 0.45))}px`}>{modelInitials(model)}</span>
{/if}

<style>
    .bat-devimg {
        display: inline-block;
        vertical-align: middle;
        object-fit: contain;
        flex: none;
    }

    img.bat-devimg {
        filter: var(--bat-devimg-filter, none);
    }

    .bat-devimg-fallback {
        display: inline-flex;
        align-items: center;
        justify-content: center;
        box-sizing: border-box;
        border: 1px solid var(--hmm-border, #8884);
        border-radius: 2px;
        background: var(--hmm-surface-2, transparent);
        color: var(--hmm-fg-muted, #888);
        line-height: 1;
        overflow: hidden;
    }
</style>
