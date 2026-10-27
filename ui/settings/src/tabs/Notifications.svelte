<script lang="ts">
    // The settings page's Notifications tab: the channel (a webhook or ntfy), which notices go
    // out, the language of their texts, and a test button. Saved through the addon's API.
    import {getKit, KitError, KitLoading, REQUEST_HEADER} from 'ccu-addon-kit/frame';

    let {active}: {active: boolean; path: string} = $props();
    const kit = getKit();

    const API = '/addons/battery/api/v1/';

    interface Notify {
        channel: '' | 'webhook' | 'ntfy';
        url?: string;
        topic?: string;
        token?: string;
        headers?: Record<string, string>;
        events: string[];
        language: 'de' | 'en';
    }

    async function call<T>(path: string, method: 'GET' | 'PUT' | 'POST', body?: unknown): Promise<T> {
        // the frame took the sid out of the address bar on load (reading location.search found
        // nothing on a CCU and the API answered 401); kit.app.url carries it, as the frame hands
        // it to the application
        const sid = kit.platform.kind === 'openccu-lite' || !kit.app ? '' : (new URL(kit.app.url, location.href).searchParams.get('sid') ?? '');
        const url = API + path + (sid ? '?sid=' + encodeURIComponent(sid) : '');
        const res = await fetch(url, {
            method,
            headers: {Accept: 'application/json', ...(method === 'GET' ? {} : {'Content-Type': 'application/json', [REQUEST_HEADER]: '1'})},
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
            const b = parsed as {message?: string; error?: string};
            throw new Error(b?.message ?? b?.error ?? res.statusText);
        }
        return parsed as T;
    }

    let n = $state<Notify>({channel: '', events: ['low', 'due', 'inbox'], language: 'de'});
    let headerLines = $state('');
    let loaded = $state(false);
    let error = $state<unknown>(null);
    let saving = $state(false);
    let testing = $state(false);
    let writable = $state(true);

    async function load(): Promise<void> {
        error = null;
        try {
            const r = await call<{notify: Notify; writable: boolean}>('settings', 'GET');
            n = {...r.notify, events: r.notify.events ?? []};
            headerLines = Object.entries(r.notify.headers ?? {})
                .map(([k, v]) => `${k}: ${v}`)
                .join('\n');
            writable = r.writable;
            loaded = true;
        } catch (e) {
            error = e;
        }
    }

    $effect(() => {
        if (active && !loaded && !error) void load();
    });

    function headers(): Record<string, string> | undefined {
        const out: Record<string, string> = {};
        for (const line of headerLines.split('\n')) {
            const i = line.indexOf(':');
            if (i > 0) out[line.slice(0, i).trim()] = line.slice(i + 1).trim();
        }
        return Object.keys(out).length ? out : undefined;
    }

    function toggle(ev: string, on: boolean): void {
        n.events = on ? [...new Set([...n.events, ev])] : n.events.filter((e) => e !== ev);
        kit.setDirty(true);
    }

    async function save(): Promise<void> {
        saving = true;
        try {
            const body: Notify = {channel: n.channel, events: n.events, language: n.language};
            if (n.channel) body.url = n.url?.trim();
            if (n.channel === 'ntfy') {
                body.topic = n.topic?.trim();
                if (n.token?.trim()) body.token = n.token.trim();
            }
            if (n.channel === 'webhook') body.headers = headers();
            await call('settings', 'PUT', {notify: body});
            kit.setDirty(false);
            kit.notify('success', kit.t('notify.saved'));
        } catch (e) {
            kit.notify('error', kit.t('notify.saveFailed', {error: (e as Error).message}));
        } finally {
            saving = false;
        }
    }

    async function test(): Promise<void> {
        testing = true;
        try {
            await call('notify/test', 'POST');
            kit.notify('success', kit.t('notify.testOk'));
        } catch (e) {
            kit.notify('error', kit.t('notify.testFailed', {error: (e as Error).message}));
        } finally {
            testing = false;
        }
    }
</script>

<section class="kit-section" data-testid="notifications">
    {#if error}
        <KitError {error} retry={load} />
    {:else if !loaded}
        <KitLoading />
    {:else}
        <p class="kit-muted">{kit.t('notify.intro')}</p>
        <div class="nt-form">
            <label for="nt-channel">{kit.t('notify.channel')}</label>
            <select id="nt-channel" class="hmm-select" bind:value={n.channel} onchange={() => kit.setDirty(true)}>
                <option value="">{kit.t('notify.channel.off')}</option>
                <option value="ntfy">ntfy</option>
                <option value="webhook">{kit.t('notify.channel.webhook')}</option>
            </select>
            {#if n.channel}
                <label for="nt-url">{kit.t('notify.url')}</label>
                <input id="nt-url" class="hmm-input" type="url" bind:value={n.url} placeholder={n.channel === 'ntfy' ? 'https://ntfy.sh' : 'https://…'} oninput={() => kit.setDirty(true)} />
            {/if}
            {#if n.channel === 'ntfy'}
                <label for="nt-topic">{kit.t('notify.topic')}</label>
                <input id="nt-topic" class="hmm-input" type="text" bind:value={n.topic} oninput={() => kit.setDirty(true)} />
                <label for="nt-token">{kit.t('notify.token')}</label>
                <input id="nt-token" class="hmm-input" type="password" bind:value={n.token} autocomplete="off" oninput={() => kit.setDirty(true)} />
            {/if}
            {#if n.channel === 'webhook'}
                <label for="nt-headers">{kit.t('notify.headers')}</label>
                <textarea id="nt-headers" class="hmm-input" rows="2" bind:value={headerLines} placeholder="X-Api-Key: …" oninput={() => kit.setDirty(true)}></textarea>
            {/if}
            <span>{kit.t('notify.events')}</span>
            <span class="nt-events">
                {#each ['low', 'due', 'soon', 'inbox'] as ev (ev)}
                    <label><input type="checkbox" checked={n.events.includes(ev)} onchange={(e) => toggle(ev, (e.currentTarget as HTMLInputElement).checked)} /> {kit.t('notify.event.' + ev)}</label>
                {/each}
            </span>
            <label for="nt-lang">{kit.t('notify.language')}</label>
            <select id="nt-lang" class="hmm-select" bind:value={n.language} onchange={() => kit.setDirty(true)}>
                <option value="de">Deutsch</option>
                <option value="en">English</option>
            </select>
        </div>
        <p class="kit-muted">{n.channel === 'webhook' ? kit.t('notify.webhookHint') : n.channel === 'ntfy' ? kit.t('notify.ntfyHint') : kit.t('notify.repeatHint')}</p>
        <div class="nt-actions">
            <button type="button" class="hmm-button kit-primary" onclick={save} disabled={saving || !writable}>{kit.t('notify.save')}</button>
            <button type="button" class="hmm-button" onclick={test} disabled={testing || !n.channel}>{kit.t('notify.test')}</button>
        </div>
        {#if !writable}<p class="kit-muted">{kit.t('notify.readOnly')}</p>{/if}
    {/if}
</section>

<style>
    .nt-form {
        display: grid;
        grid-template-columns: max-content minmax(14em, 32em);
        gap: 8px 12px;
        align-items: center;
        margin: 8px 0;
    }

    .nt-events {
        display: flex;
        flex-wrap: wrap;
        gap: 4px 14px;
    }

    .nt-actions {
        display: flex;
        gap: 8px;
        margin-top: 8px;
    }

    textarea.hmm-input {
        font-family: var(--hmm-font-mono);
        resize: vertical;
    }
</style>
