<script lang="ts">
    // The app: Devices (with a device's detail), Inbox, Batteries, Statistics. The database
    // location and the service live on the addon's settings page (the ccu-addon-kit frame), not
    // here.
    import {Notices, Tabs, ThemeSwitch} from 'occulite-ui';
    import {i18n, t} from 'occulite-ui/i18n';
    import {NoticesStore} from 'occulite-ui/notices';
    import {cycleTheme, theme} from 'occulite-ui/theme';

    import BatteriesPage from './BatteriesPage.svelte';
    import DeviceDetail from './DeviceDetail.svelte';
    import DevicesPage from './DevicesPage.svelte';
    import InboxPage from './InboxPage.svelte';
    import StatisticsPage from './StatisticsPage.svelte';
    import {get} from './api.js';
    import type {DeviceView, StorageHealth} from './model.js';

    const notices = new NoticesStore();
    let tab = $state('devices');
    let detail = $state<DeviceView | undefined>();
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

    // a device's detail is re-read when the table reloads, so it shows the current values
    async function openDetail(d: DeviceView) {
        detail = d;
        tab = 'devices';
    }

    async function openDetailByKey(key: string) {
        try {
            const r = await get<{devices: DeviceView[]}>(`api/v1/devices?lang=${i18n.language}`);
            const d = r.devices.find((x) => `${x.interface}.${x.address}` === key);
            if (d) openDetail(d);
        } catch {
            /* stays on the page */
        }
    }

    async function refreshDetail() {
        if (!detail) return;
        try {
            const r = await get<{devices: DeviceView[]}>(`api/v1/devices?lang=${i18n.language}`);
            const d = r.devices.find((x) => x.interface === detail?.interface && x.address === detail?.address);
            if (d) detail = d;
        } catch {
            /* keeps the old view */
        }
    }

    const tabs = $derived([
        {id: 'devices', label: t('Devices')},
        {id: 'inbox', label: t('Inbox'), ...(health?.inbox ? {badge: health.inbox} : {})},
        {id: 'batteries', label: t('Batteries')},
        {id: 'stats', label: t('Statistics')},
    ]);
</script>

<div class="bat-app" lang={i18n.language}>
    <header class="bat-header">
        <h1>{t('Battery')}</h1>
        <Tabs {tabs} bind:active={tab} label={t('Battery')} onselect={() => (detail = undefined)} />
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
            {#if detail}
                <DeviceDetail device={detail} {notices} onback={() => (detail = undefined)} onchanged={() => (loadHealth(), refreshDetail())} />
            {:else}
                <DevicesPage {notices} inbox={health?.inbox ?? 0} onshowinbox={() => (tab = 'inbox')} onopen={openDetail} onchanged={loadHealth} />
            {/if}
        {:else if tab === 'inbox'}
            <InboxPage {notices} onchanged={loadHealth} />
        {:else if tab === 'batteries'}
            <BatteriesPage />
        {:else}
            <StatisticsPage onopen={openDetailByKey} />
        {/if}
    </main>
    <Notices notices={notices.visible} ondismiss={(id) => notices.dismiss(id)} />
</div>
