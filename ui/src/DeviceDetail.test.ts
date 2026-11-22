import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/svelte';
import {addMessages, i18n} from 'occulite-ui/i18n';
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest';

// uPlot (the voltage chart) asks matchMedia when it is loaded
vi.hoisted(() => {
    globalThis.matchMedia ??= ((q: string) => ({matches: false, media: q, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}})) as unknown as typeof matchMedia;
});

import DeviceDetail from './DeviceDetail.svelte';
import {MESSAGES} from './messages.js';
import type {DeviceView, Profile} from './model.js';

const mp3p = (profile: Partial<Profile>, power = 'battery-or-dc'): DeviceView => ({
    interface: 'HmIP-RF',
    address: 'MP3P',
    name: 'Gong',
    rooms: [],
    model: 'HmIP-MP3P',
    power,
    voltage: 5.0,
    low_bat: false,
    unreach: false,
    first_seen: 0,
    profile: {source: 'table', power: 'battery-or-dc', table_power: 'battery-or-dc', slots: [{count: 3, type: 'C'}], ...profile},
});

const notices = {push: vi.fn()} as unknown as import('occulite-ui/notices').NoticesStore;

type Call = {url: string; method: string; body?: string};

function serve(override: Record<string, unknown> | null, model: Record<string, unknown> = {in_table: true}): Call[] {
    const calls: Call[] = [];
    vi.stubGlobal(
        'fetch',
        vi.fn(async (url: string, init?: RequestInit) => {
            const u = String(url);
            calls.push({url: u, method: init?.method ?? 'GET', body: init?.body as string | undefined});
            let body: unknown = {};
            if (u.includes('/override')) body = init?.method && init.method !== 'GET' ? {ok: true} : {override};
            else if (u.includes('api/v1/models/')) body = model;
            else if (u.includes('api/v1/samples')) body = {samples: []};
            else if (u.includes('api/v1/changes')) body = {changes: []};
            return new Response(JSON.stringify(body), {status: 200, headers: {'Content-Type': 'application/json'}});
        }),
    );
    return calls;
}

describe('DeviceDetail: the power supply choice', () => {
    beforeEach(() => {
        addMessages(MESSAGES);
        i18n.language = 'de';
        vi.stubGlobal(
            'ResizeObserver',
            class {
                observe() {}
                unobserve() {}
                disconnect() {}
            },
        );
    });
    afterEach(() => {
        cleanup();
        vi.unstubAllGlobals();
    });

    it('is automatic by default and says when it found the power supply unit', async () => {
        serve(null);
        render(DeviceDetail, {device: mp3p({power: 'external', auto: true}, 'external'), notices, onback: () => {}});
        const sel = (await screen.findByTestId('supply')) as HTMLSelectElement;
        await waitFor(() => expect(sel.value).toBe('auto'));
        expect(sel.selectedOptions[0]?.textContent).toBe('Automatisch (Netzteil)');
        expect(screen.getByTestId('supply-auto').textContent).toContain('Netzteil automatisch erkannt');
    });

    it('on its batteries: automatic (batteries), no note', async () => {
        serve(null);
        render(DeviceDetail, {device: mp3p({}), notices, onback: () => {}});
        const sel = (await screen.findByTestId('supply')) as HTMLSelectElement;
        await waitFor(() => expect(sel.value).toBe('auto'));
        expect(sel.selectedOptions[0]?.textContent).toBe('Automatisch (Batterien)');
        expect(screen.queryByTestId('supply-auto')).toBeNull();
    });

    it('the manual choice is written as the override and shown as such', async () => {
        const calls = serve({power: 'battery'});
        render(DeviceDetail, {device: mp3p({power: 'battery', source: 'override'}, 'battery'), notices, onback: () => {}});
        const sel = (await screen.findByTestId('supply')) as HTMLSelectElement;
        await waitFor(() => expect(sel.value).toBe('batteries'));
        expect(screen.queryByTestId('supply-auto')).toBeNull();

        await fireEvent.change(sel, {target: {value: 'external'}});
        await waitFor(() => expect(calls.some((c) => c.method === 'PUT' && c.url.endsWith('/override'))).toBe(true));
        expect(JSON.parse(calls.find((c) => c.method === 'PUT')!.body!)).toEqual({power: 'external'});

        await fireEvent.change(sel, {target: {value: 'auto'}});
        await waitFor(() => expect(calls.some((c) => c.method === 'DELETE' && c.url.endsWith('/override'))).toBe(true));
    });

    it('is not offered for a battery-only model', async () => {
        serve(null);
        render(DeviceDetail, {device: {...mp3p({power: 'battery', table_power: 'battery'}, 'battery'), model: 'HmIP-WRC2'}, notices, onback: () => {}});
        await screen.findByTestId('device-detail');
        await new Promise((r) => setTimeout(r, 20));
        expect(screen.queryByTestId('supply')).toBeNull();
    });
});

describe('DeviceDetail: the battery editor', () => {
    beforeEach(() => {
        // jsdom's <dialog> has no showModal
        HTMLDialogElement.prototype.showModal ??= function (this: HTMLDialogElement) {
            this.open = true;
        };
        HTMLDialogElement.prototype.close ??= function (this: HTMLDialogElement) {
            this.open = false;
        };
        addMessages(MESSAGES);
        i18n.language = 'de';
        vi.stubGlobal('ResizeObserver', class {
            observe() {}
            unobserve() {}
            disconnect() {}
        });
    });
    afterEach(() => {
        cleanup();
        vi.unstubAllGlobals();
    });

    it('writes the model override for all devices of the model, only what differs from the table', async () => {
        const calls = serve(null, {in_table: true, row: {power: 'battery', slots: [{count: 2, type: 'AAA'}]}, profile: {power: 'battery', slots: [{count: 2, type: 'AAA'}]}});
        const wrc2: DeviceView = {...mp3p({source: 'table', power: 'battery', table_power: 'battery', slots: [{count: 2, type: 'AAA'}]}, 'battery'), model: 'HmIP-WRC2', address: 'WRC2'};
        render(DeviceDetail, {device: wrc2, notices, onback: () => {}});
        await fireEvent.click(await screen.findByTestId('edit-batteries'));
        await fireEvent.click(await screen.findByLabelText('alle Geräte des Modells HmIP-WRC2'));
        await fireEvent.click(screen.getByTestId('editor-outdoor'));
        await fireEvent.click(screen.getByTestId('editor-save'));
        await waitFor(() => expect(calls.some((c) => c.method === 'PUT' && c.url.includes('/models/HmIP-WRC2/override'))).toBe(true));
        expect(JSON.parse(calls.find((c) => c.method === 'PUT')!.body!)).toEqual({outdoor: true});
    });
});
