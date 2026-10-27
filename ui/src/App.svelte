<script lang="ts">
    import {Notices, Tabs, ThemeSwitch} from 'occulite-ui';
    import {i18n, t} from 'occulite-ui/i18n';
    import {NoticesStore} from 'occulite-ui/notices';
    import {cycleTheme, theme} from 'occulite-ui/theme';

    import DevicesPage from './DevicesPage.svelte';
    import InboxPage from './InboxPage.svelte';
    import StoragePage from './StoragePage.svelte';
    import {get} from './api.js';
    import type {StorageHealth} from './model.js';

    const notices = new NoticesStore();
    let tab = $state('devices');
    let health = $state<{ok: boolean; recorder: string; storage: StorageHealth; inbox?: number} | undefined>();

    async function loadHealth() {
        try {
            health = await get('api/v1/health');
        } catch {
            health = undefined;
        }
    }

    $effect(() => {
        loadHealth();
        const id = setInterval(loadHealth, 30_000);
        return () => clearInterval(id);
    });

    const tabs = $derived([
        {id: 'devices', label: t('Devices')},
        {id: 'inbox', label: t('Inbox')},
        {id: 'storage', label: t('Storage')},
    ]);
</script>

<div class="bat-app" lang={i18n.language}>
    <header class="bat-header">
        <h1>{t('Battery')}</h1>
        <Tabs {tabs} bind:active={tab} label={t('Battery')} />
        <span class="bat-spacer"></span>
        {#if !theme.followsShell}
            <ThemeSwitch theme={theme.choice} onclick={cycleTheme} />
        {/if}
    </header>
    {#if health && health.storage.state !== 'ok'}
        <div class="bat-banner" role="status">
            <strong>{t('storage.title')}: {t('storage.' + health.storage.state)}.</strong>
            {health.storage.state === 'unavailable' ? t('storage.unavailableHint') : t('storage.degradedHint')}
            {#if health.storage.error}<span class="bat-muted bat-mono"> ({health.storage.error})</span>{/if}
        </div>
    {/if}
    {#if health && health.recorder !== 'live'}
        <div class="bat-banner info" role="status">{t('The recorder is not live ({state}).', {state: health.recorder})}</div>
    {/if}
    <main class="bat-page">
        {#if tab === 'devices'}
            <DevicesPage {notices} inbox={health?.inbox ?? 0} onshowinbox={() => (tab = 'inbox')} />
        {:else if tab === 'inbox'}
            <InboxPage {notices} onchanged={loadHealth} />
        {:else}
            <StoragePage {notices} onchanged={loadHealth} />
        {/if}
    </main>
    <Notices notices={notices.visible} ondismiss={(id) => notices.dismiss(id)} />
</div>
