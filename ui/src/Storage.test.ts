// The settings page's Database tab (settings/src/tabs/Storage.svelte), with the kit's frame
// context stubbed: which sticks it offers, how it names them, and that a failed move says why.
import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/svelte';
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest';

import {MESSAGES} from './messages.js';
import type {Candidate, StorageHealth} from './model.js';

const notify = vi.fn();

vi.mock('ccu-addon-kit/frame', async (original) => {
    const actual = await original<typeof import('ccu-addon-kit/frame')>();
    const text = (key: string, params: Record<string, string> = {}) => {
        const m = (MESSAGES as Record<string, {de?: unknown; en?: unknown}>)[key];
        let s = typeof m?.en === 'string' ? m.en : key;
        for (const [k, v] of Object.entries(params)) s = s.replaceAll(`{${k}}`, v);
        return s;
    };
    return {
        ...actual,
        getKit: () => ({t: text, lang: 'en', notify, platform: {kind: 'ccu3'}, session: {level: 'admin'}, app: undefined}),
    };
});

import Storage from '../settings/src/tabs/Storage.svelte';

const health: StorageHealth = {
    state: 'ok',
    path: '/usr/local/addons/battery/var/battery.db',
    location: {location: 'userfs'},
    pending: 0,
    dropped: 0,
    size_bytes: 4096,
    in_backup: true,
} as StorageHealth;

const stick = (p: Partial<Candidate>): Candidate => ({id: 'usb:X', name: '', path: '/media/usb1', free_bytes: 64 << 20, total_bytes: 64 << 20, allowed: true, in_backup: false, current: false, ...p});

const CANDIDATES: Candidate[] = [
    {id: 'userfs', name: 'userfs', path: '/usr/local/addons/battery/var', free_bytes: 1 << 30, total_bytes: 1 << 30, allowed: true, in_backup: true, current: true},
    stick({id: 'usb:BATTERY', name: 'BATTERY', kind: 'label', path: '/media/usb1'}),
    stick({id: 'usb-uuid:5E7A-01FF', kind: 'uuid', uuid: '5E7A-01FF', path: '/media/usb2'}),
    stick({id: 'usb-path:/media/usb3', kind: 'path', path: '/media/usb3'}),
    stick({id: 'usb-uuid:AAAA-0000', kind: 'uuid', uuid: 'AAAA-0000', path: '/media/usb4', allowed: false, reason: 'read-only'}),
];

let moves: string[] = [];

function serve(moveAnswer: {status: number; body: unknown}) {
    moves = [];
    vi.stubGlobal(
        'fetch',
        vi.fn(async (url: string, init?: RequestInit) => {
            const u = String(url);
            let status = 200;
            let body: unknown = {};
            if (u.endsWith('/storage')) body = {health, candidates: CANDIDATES};
            else if (u.endsWith('/storage/check')) body = {ok: true, path: '/media/usb3/battery/battery.db'};
            else if (u.endsWith('/storage/move')) {
                moves.push(String(init?.body));
                status = moveAnswer.status;
                body = moveAnswer.body;
            }
            return new Response(JSON.stringify(body), {status, headers: {'Content-Type': 'application/json'}});
        }),
    );
}

describe('the Database tab: USB sticks', () => {
    beforeEach(() => {
        notify.mockClear();
        // jsdom's <dialog> has no showModal
        HTMLDialogElement.prototype.showModal ??= function (this: HTMLDialogElement) {
            this.open = true;
        };
        HTMLDialogElement.prototype.close ??= function (this: HTMLDialogElement) {
            this.open = false;
        };
    });
    afterEach(() => {
        cleanup();
        vi.unstubAllGlobals();
    });

    it('offers every stick, with or without a label, and names it by label, UUID or mount point', async () => {
        serve({status: 200, body: {ok: true, storage: health}});
        render(Storage, {active: true, path: ''});
        const select = (await screen.findByTestId('storage-target')) as HTMLSelectElement;
        const options = [...select.options].map((o) => [o.value, o.textContent?.trim().replace(/\s+/g, ' '), o.disabled]);
        expect(options).toEqual([
            ['userfs', 'User partition (default) – 1.00 GB free', false],
            ['usb:BATTERY', 'USB stick “BATTERY” – 64.0 MB free', false],
            ['usb-uuid:5E7A-01FF', 'USB stick without a label (UUID 5E7A-01FF) – 64.0 MB free', false],
            ['usb-path:/media/usb3', 'USB stick without a label (/media/usb3) – 64.0 MB free', false],
            ['usb-uuid:AAAA-0000', 'USB stick without a label (UUID AAAA-0000) – read-only – 64.0 MB free', true],
        ]);
        // no stick is refused for having no label any more
        expect(screen.queryByText(/cannot be chosen/)).toBeNull();
        // a stick known by its mount point says what that means, once chosen
        expect(screen.queryByTestId('storage-path-note')).toBeNull();
        await fireEvent.change(select, {target: {value: 'usb-path:/media/usb3'}});
        expect((await screen.findByTestId('storage-path-note')).textContent).toContain('/media/usb3');
        // the folder field belongs to every stick
        expect(screen.getByLabelText('Folder on the stick')).toBeTruthy();
    });

    it('keeps the reason on the page when a move fails', async () => {
        serve({status: 409, body: {error: 'move-failed', message: '/media/usb2/battery is not writable: permission denied'}});
        render(Storage, {active: true, path: ''});
        const select = (await screen.findByTestId('storage-target')) as HTMLSelectElement;
        await fireEvent.change(select, {target: {value: 'usb-uuid:5E7A-01FF'}});
        const moveButtons = () => screen.getAllByRole('button', {name: 'Move', hidden: true});
        await fireEvent.click(moveButtons()[0]!);
        await waitFor(() => expect(moveButtons().length).toBe(2));
        await fireEvent.click(moveButtons()[1]!);
        const alert = await screen.findByTestId('storage-move-error');
        expect(alert.textContent).toContain('Move failed: /media/usb2/battery is not writable: permission denied');
        expect(alert.textContent).toContain('unchanged');
        expect(JSON.parse(moves[0] ?? '')).toEqual({location: 'usb-uuid:5E7A-01FF', folder: 'battery', keep_old: true});
        expect(notify).toHaveBeenCalledWith('error', expect.stringContaining('permission denied'));
        // the next choice clears it
        await fireEvent.change(select, {target: {value: 'usb:BATTERY'}});
        await waitFor(() => expect(screen.queryByTestId('storage-move-error')).toBeNull());
    });
});
