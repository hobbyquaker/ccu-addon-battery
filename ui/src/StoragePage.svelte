<script lang="ts">
    import {Dialog} from 'occulite-ui';
    import {i18n, t} from 'occulite-ui/i18n';
    import type {NoticesStore} from 'occulite-ui/notices';

    import {ApiError, get, post} from './api.js';
    import {formatBytes, relativeTime, type Candidate, type StorageHealth} from './model.js';

    let {notices, onchanged}: {notices: NoticesStore; onchanged?: () => void} = $props();

    let health = $state<StorageHealth | undefined>();
    let candidates = $state<Candidate[]>([]);
    let error = $state('');
    let target = $state('userfs');
    let folder = $state('battery');
    let checking = $state(false);
    let checkResult = $state<{ok: boolean; path?: string; error?: string} | undefined>();
    let confirmOpen = $state(false);
    let keepOld = $state(true);
    let moving = $state(false);

    async function load() {
        try {
            const r = await get<{health: StorageHealth; candidates: Candidate[]}>('api/v1/storage');
            health = r.health;
            candidates = r.candidates;
            const cur = r.candidates.find((c) => c.current);
            if (cur && cur.id !== 'usb:') target = cur.id;
            if (r.health.location.folder) folder = r.health.location.folder;
            error = '';
        } catch (e) {
            error = (e as Error).message;
        }
    }

    $effect(() => {
        load();
    });

    const lang = $derived(i18n.language);
    const sticks = $derived(candidates.filter((c) => c.id.startsWith('usb:')));
    const isUsb = $derived(target.startsWith('usb:'));
    const body = $derived({location: target, folder: isUsb ? folder : undefined});
    const current = $derived(candidates.find((c) => c.current)?.id === target && (!isUsb || folder === (health?.location.folder || 'battery')));

    function candidateLabel(c: Candidate): string {
        const name = c.id === 'userfs' ? t('storage.candidate.userfs') : t('storage.candidate.usb', {label: c.name});
        const parts = [name];
        if (c.reason === 'no-label') parts.push(t('storage.candidate.noLabel'));
        if (c.reason === 'read-only') parts.push(t('storage.candidate.readOnly'));
        if (c.free_bytes) parts.push(t('storage.candidate.free', {free: formatBytes(c.free_bytes)}));
        return parts.join(' – ');
    }

    async function check() {
        checking = true;
        checkResult = undefined;
        try {
            checkResult = await post('api/v1/storage/check', body);
        } catch (e) {
            checkResult = {ok: false, error: (e as Error).message};
        } finally {
            checking = false;
        }
    }

    async function move() {
        moving = true;
        try {
            const r = await post<{ok: boolean; storage: StorageHealth}>('api/v1/storage/move', {...body, keep_old: keepOld});
            confirmOpen = false;
            notices.push('info', t('storage.move.done', {path: r.storage.path}));
            checkResult = undefined;
            await load();
            onchanged?.();
        } catch (e) {
            const msg = e instanceof ApiError && e.status === 403 ? t('storage.admin') : (e as Error).message;
            notices.push('error', t('storage.move.failed', {error: msg}));
        } finally {
            moving = false;
        }
    }
</script>

{#if error}
    <div class="bat-banner" role="alert">{t('Could not load: {error}', {error})}</div>
{/if}
{#if health}
    <section class="bat-card">
        <h2>{t('storage.title')}</h2>
        <dl class="bat-kv">
            <dt>{t('storage.location')}</dt>
            <dd>
                {health.location.location === 'userfs' ? t('storage.candidate.userfs') : t('storage.candidate.usb', {label: health.location.location.slice(4)})}
                {#if health.location.folder}<span class="bat-mono"> /{health.location.folder}</span>{/if}
            </dd>
            <dt>{t('storage.path')}</dt>
            <dd class="bat-mono">{health.path || '–'}</dd>
            <dt>{t('storage.state')}</dt>
            <dd><span class="bat-pill {health.state}">{t('storage.' + health.state)}</span>{#if health.error}<span class="bat-muted"> {health.error}</span>{/if}</dd>
            <dt>{t('storage.size')}</dt>
            <dd>{formatBytes(health.size_bytes)}</dd>
            <dt>{t('storage.pending')}</dt>
            <dd>{health.pending}</dd>
            {#if health.dropped}
                <dt>{t('storage.dropped')}</dt>
                <dd>{health.dropped}</dd>
            {/if}
            <dt>{t('storage.lastFlush')}</dt>
            <dd>{health.last_flush ? relativeTime(Date.parse(health.last_flush), Date.now(), lang) : t('time.never')}</dd>
        </dl>
        <p class="bat-muted">{health.in_backup ? t('storage.inBackup') : t('storage.notInBackup')}</p>
    </section>

    <section class="bat-card">
        <h2>{t('storage.move.title')}</h2>
        <p class="bat-muted">{t('storage.move.intro')}</p>
        <div class="bat-form">
            <label for="bat-target">{t('storage.move.target')}</label>
            <select id="bat-target" bind:value={target} onchange={() => (checkResult = undefined)}>
                {#each candidates as c (c.id)}
                    <option value={c.id} disabled={!c.allowed}>{candidateLabel(c)}</option>
                {/each}
            </select>
            {#if isUsb}
                <label for="bat-folder">{t('storage.move.folder')}</label>
                <input id="bat-folder" type="text" bind:value={folder} pattern="[A-Za-z0-9][A-Za-z0-9._\-]*(/[A-Za-z0-9][A-Za-z0-9._\-]*)*" oninput={() => (checkResult = undefined)} />
            {/if}
        </div>
        {#if sticks.length === 0}
            <p class="bat-muted">{t('storage.noSticks')}</p>
        {/if}
        {#if checkResult}
            <p role="status">
                {checkResult.ok ? t('storage.move.checkOk', {path: checkResult.path ?? ''}) : t('storage.move.checkFailed', {error: checkResult.error ?? ''})}
            </p>
        {/if}
        <div class="bat-actions">
            <button type="button" onclick={check} disabled={checking || current || health.state !== 'ok'}>{checking ? t('storage.move.checking') : t('storage.move.check')}</button>
            <button type="button" class="primary" onclick={() => (confirmOpen = true)} disabled={current || health.state !== 'ok' || (checkResult !== undefined && !checkResult.ok)}>
                {t('storage.move.move')}
            </button>
        </div>
    </section>

    <Dialog bind:open={confirmOpen} title={t('storage.move.title')} width="30rem" closable={!moving} onclose={() => (confirmOpen = false)}>
        <p>{t('storage.move.confirm', {path: checkResult?.path ?? (isUsb ? `${sticks.find((s) => s.id === target)?.path ?? ''}/${folder}/battery.db` : health.path)})}</p>
        <p class="bat-muted">{target === 'userfs' ? t('storage.inBackup') : t('storage.notInBackup')}</p>
        <label><input type="checkbox" bind:checked={keepOld} /> {t('storage.move.keepOld')}</label>
        {#snippet buttons()}
            <div class="bat-dialog-buttons">
                <button type="button" onclick={() => (confirmOpen = false)} disabled={moving}>{t('Cancel')}</button>
                <button type="button" class="primary" onclick={move} disabled={moving}>{moving ? t('storage.move.moving') : t('storage.move.move')}</button>
            </div>
        {/snippet}
    </Dialog>
{/if}
