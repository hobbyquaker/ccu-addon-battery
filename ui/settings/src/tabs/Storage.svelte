<script lang="ts">
    // The settings page's own tab: where the database lives (health, the candidates, a check, the
    // move with "keep the old file"). It talks to the addon's Go backend at /addons/battery/api/v1/
    // - the same origin, the session the platform hands over - never to a CGI of its own.
    import {Dialog} from 'occulite-ui';
    import {getKit, KitError, KitLoading, REQUEST_HEADER} from 'ccu-addon-kit/frame';

    import {formatBytes, relativeTime, type Candidate, type StorageHealth} from '../../../src/model.js';

    let {active}: {active: boolean; path: string} = $props();
    const kit = getKit();

    const API = '/addons/battery/api/v1/';

    class ApiError extends Error {
        status: number;
        code: string;
        constructor(status: number, code: string, message: string) {
            super(message);
            this.status = status;
            this.code = code;
        }
    }

    async function call<T>(path: string, body?: unknown): Promise<T> {
        const sid = kit.platform.kind === 'openccu-lite' ? '' : new URLSearchParams(location.search).get('sid') ?? '';
        const url = API + path + (sid ? (path.includes('?') ? '&' : '?') + 'sid=' + encodeURIComponent(sid) : '');
        const res = await fetch(url, {
            method: body === undefined ? 'GET' : 'POST',
            headers: {Accept: 'application/json', ...(body === undefined ? {} : {'Content-Type': 'application/json', [REQUEST_HEADER]: '1'})},
            credentials: 'same-origin',
            cache: 'no-store',
            body: body === undefined ? undefined : JSON.stringify(body),
        });
        const text = await res.text();
        let parsed: unknown = text;
        try {
            parsed = text ? JSON.parse(text) : {};
        } catch {
            /* not JSON */
        }
        if (!res.ok) {
            const b = parsed as {error?: string; message?: string};
            throw new ApiError(res.status, b?.error ?? String(res.status), b?.message ?? res.statusText);
        }
        return parsed as T;
    }

    let health = $state<StorageHealth | undefined>();
    let candidates = $state<Candidate[]>([]);
    let error = $state<unknown>(null);
    let loaded = $state(false);
    let target = $state('userfs');
    let folder = $state('battery');
    let checking = $state(false);
    let checkResult = $state<{ok: boolean; path?: string; error?: string} | undefined>();
    let confirmOpen = $state(false);
    let keepOld = $state(true);
    let moving = $state(false);

    async function load(): Promise<void> {
        error = null;
        try {
            const r = await call<{health: StorageHealth; candidates: Candidate[]}>('storage');
            health = r.health;
            candidates = r.candidates;
            const cur = r.candidates.find((c) => c.current);
            if (cur && cur.id !== 'usb:') target = cur.id;
            if (r.health.location.folder) folder = r.health.location.folder;
            loaded = true;
        } catch (e) {
            error = e;
        }
    }

    $effect(() => {
        if (active && !loaded && !error) void load();
    });

    const sticks = $derived(candidates.filter((c) => c.id.startsWith('usb:')));
    const isUsb = $derived(target.startsWith('usb:'));
    const body = $derived({location: target, folder: isUsb ? folder : undefined});
    const current = $derived(candidates.find((c) => c.current)?.id === target && (!isUsb || folder === (health?.location.folder || 'battery')));
    const admin = $derived(kit.session.level === 'admin' || kit.platform.kind !== 'openccu-lite');

    function candidateLabel(c: Candidate): string {
        const name = c.id === 'userfs' ? kit.t('storage.candidate.userfs') : kit.t('storage.candidate.usb', {label: c.name});
        const parts = [name];
        if (c.reason === 'no-label') parts.push(kit.t('storage.candidate.noLabel'));
        if (c.reason === 'read-only') parts.push(kit.t('storage.candidate.readOnly'));
        if (c.free_bytes) parts.push(kit.t('storage.candidate.free', {free: formatBytes(c.free_bytes)}));
        return parts.join(' – ');
    }

    async function check(): Promise<void> {
        checking = true;
        checkResult = undefined;
        try {
            checkResult = await call('storage/check', body);
        } catch (e) {
            checkResult = {ok: false, error: (e as Error).message};
        } finally {
            checking = false;
        }
    }

    async function move(): Promise<void> {
        moving = true;
        try {
            const r = await call<{ok: boolean; storage: StorageHealth}>('storage/move', {...body, keep_old: keepOld});
            confirmOpen = false;
            kit.notify('success', kit.t('storage.move.done', {path: r.storage.path}));
            checkResult = undefined;
            await load();
        } catch (e) {
            const msg = e instanceof ApiError && e.status === 403 ? kit.t('storage.admin') : (e as Error).message;
            kit.notify('error', kit.t('storage.move.failed', {error: msg}));
        } finally {
            moving = false;
        }
    }
</script>

<section class="kit-section" data-testid="storage">
    {#if error}
        <KitError {error} retry={load} />
    {:else if !loaded || !health}
        <KitLoading />
    {:else}
        <h3 class="st-h">{kit.t('storage.title')}</h3>
        <dl class="st-kv">
            <dt>{kit.t('storage.location')}</dt>
            <dd>
                {health.location.location === 'userfs' ? kit.t('storage.candidate.userfs') : kit.t('storage.candidate.usb', {label: health.location.location.slice(4)})}
                {#if health.location.folder}<span class="st-mono"> /{health.location.folder}</span>{/if}
            </dd>
            <dt>{kit.t('storage.path')}</dt>
            <dd class="st-mono">{health.path || '–'}</dd>
            <dt>{kit.t('storage.state')}</dt>
            <dd><span class="st-pill {health.state}">{kit.t('storage.' + health.state)}</span>{#if health.error}<span class="kit-muted"> {health.error}</span>{/if}</dd>
            <dt>{kit.t('storage.size')}</dt>
            <dd>{formatBytes(health.size_bytes)}</dd>
            <dt>{kit.t('storage.pending')}</dt>
            <dd>{health.pending}</dd>
            {#if health.dropped}
                <dt>{kit.t('storage.dropped')}</dt>
                <dd>{health.dropped}</dd>
            {/if}
            <dt>{kit.t('storage.lastFlush')}</dt>
            <dd>{health.last_flush ? relativeTime(Date.parse(health.last_flush), Date.now(), kit.lang) : kit.t('time.never')}</dd>
        </dl>
        <p class="kit-muted">{health.in_backup ? kit.t('storage.inBackup') : kit.t('storage.notInBackup')}</p>

        <h3 class="st-h">{kit.t('storage.move.title')}</h3>
        <p class="kit-muted">{kit.t('storage.move.intro')}</p>
        <div class="st-form">
            <label for="st-target">{kit.t('storage.move.target')}</label>
            <select id="st-target" class="hmm-select" bind:value={target} onchange={() => (checkResult = undefined)}>
                {#each candidates as c (c.id)}
                    <option value={c.id} disabled={!c.allowed}>{candidateLabel(c)}</option>
                {/each}
            </select>
            {#if isUsb}
                <label for="st-folder">{kit.t('storage.move.folder')}</label>
                <input id="st-folder" class="hmm-input" type="text" bind:value={folder} pattern="[A-Za-z0-9][A-Za-z0-9._\-]*(/[A-Za-z0-9][A-Za-z0-9._\-]*)*" oninput={() => (checkResult = undefined)} />
            {/if}
        </div>
        {#if sticks.length === 0}
            <p class="kit-muted">{kit.t('storage.noSticks')}</p>
        {/if}
        {#if !admin}
            <p class="kit-muted">{kit.t('storage.admin')}</p>
        {/if}
        <p role="status" class="st-status">
            {#if checkResult}{checkResult.ok ? kit.t('storage.move.checkOk', {path: checkResult.path ?? ''}) : kit.t('storage.move.checkFailed', {error: checkResult.error ?? ''})}{:else}&nbsp;{/if}
        </p>
        <div class="st-actions">
            <button type="button" class="hmm-button" onclick={check} disabled={checking || current || health.state !== 'ok'}>{checking ? kit.t('storage.move.checking') : kit.t('storage.move.check')}</button>
            <button type="button" class="hmm-button kit-primary" onclick={() => (confirmOpen = true)} disabled={!admin || current || health.state !== 'ok' || (checkResult !== undefined && !checkResult.ok)}>
                {kit.t('storage.move.move')}
            </button>
        </div>

        <Dialog bind:open={confirmOpen} title={kit.t('storage.move.title')} width="30rem" closable={!moving} onclose={() => (confirmOpen = false)}>
            <p>{kit.t('storage.move.confirm', {path: checkResult?.path ?? (isUsb ? `${sticks.find((s) => s.id === target)?.path ?? ''}/${folder}/battery.db` : health.path)})}</p>
            <p class="kit-muted">{target === 'userfs' ? kit.t('storage.inBackup') : kit.t('storage.notInBackup')}</p>
            <label><input type="checkbox" bind:checked={keepOld} /> {kit.t('storage.move.keepOld')}</label>
            {#snippet buttons()}
                <div class="st-actions">
                    <button type="button" class="hmm-button" onclick={() => (confirmOpen = false)} disabled={moving}>{kit.t('cancel')}</button>
                    <button type="button" class="hmm-button kit-primary" onclick={move} disabled={moving}>{moving ? kit.t('storage.move.moving') : kit.t('storage.move.move')}</button>
                </div>
            {/snippet}
        </Dialog>
    {/if}
</section>

<style>
    .st-h {
        font-size: 1rem;
        margin: 12px 0 6px;
    }

    .st-kv {
        display: grid;
        grid-template-columns: max-content 1fr;
        gap: 4px 16px;
        margin: 0;
    }

    .st-kv dt {
        color: var(--hmm-fg-muted);
    }

    .st-kv dd {
        margin: 0;
    }

    .st-mono {
        font-family: var(--hmm-font-mono);
    }

    .st-form {
        display: grid;
        grid-template-columns: max-content minmax(12em, 28em);
        gap: 6px 12px;
        align-items: center;
        margin: 8px 0;
    }

    .st-actions {
        display: flex;
        gap: 8px;
        margin-top: 8px;
    }

    .st-status {
        min-height: 1.4em;
        margin: 4px 0;
    }

    /* a pill keeps its box across states */
    .st-pill {
        display: inline-block;
        box-sizing: border-box;
        width: 7.5em;
        padding: 0 8px;
        border-radius: 999px;
        font-size: 0.85em;
        line-height: 1.6;
        text-align: center;
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
        border: 1px solid var(--hmm-border);
        color: var(--hmm-fg);
    }

    .st-pill.ok {
        border-color: var(--hmm-ok);
        color: var(--hmm-ok);
    }

    .st-pill.degraded,
    .st-pill.unavailable {
        border-color: var(--hmm-warn);
        color: var(--hmm-warn);
    }
</style>
