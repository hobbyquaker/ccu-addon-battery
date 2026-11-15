import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/svelte';
import {addMessages, i18n} from 'occulite-ui/i18n';
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest';

import InboxPage from './InboxPage.svelte';
import {MESSAGES} from './messages.js';
import type {Change} from './model.js';

const until = Date.UTC(2026, 10, 16, 13, 30);

const change = (p: Partial<Change>): Change => ({
    id: 1,
    interface: 'HmIP-RF',
    address: '000193C99510F3',
    name: 'Fernbedienung Flur',
    model: 'HmIP-WRC2',
    at: until - 86_400_000,
    kind: 'detected',
    confidence: '',
    rule: 'A+B',
    status: 'candidate',
    evidence: {triggers: ['A', 'B'], voltage_before: 2.8, voltage_after: 3.1, window_s: 86400, last_trigger: until - 86_400_000},
    batteries: [],
    note: '',
    source: 'detector',
    created_at: 0,
    updated_at: 0,
    ...p,
});

let calls: {method: string; url: string; body?: string}[] = [];

function serve(inbox: Change[]) {
    calls = [];
    vi.stubGlobal(
        'fetch',
        vi.fn(async (url: string, init?: RequestInit) => {
            calls.push({method: init?.method ?? 'GET', url: String(url), body: init?.body as string | undefined});
            let body: unknown = {};
            if (String(url).includes('api/v1/changes?status=candidate,pending')) body = {changes: inbox};
            else if (String(url).includes('api/v1/changes?')) body = {changes: []};
            else if (String(url).includes('api/v1/devices')) body = {devices: []};
            else if (init?.method === 'PATCH') body = {...inbox[0], status: JSON.parse(String(init.body)).status};
            return new Response(JSON.stringify(body), {status: 200, headers: {'Content-Type': 'application/json'}});
        }),
    );
}

const notices = {push: vi.fn()} as unknown as import('occulite-ui/notices').NoticesStore;

describe('InboxPage', () => {
    beforeEach(() => {
        addMessages(MESSAGES);
        i18n.language = 'en';
        vi.useFakeTimers({toFake: ['Date'], now: until - 3_600_000});
        // jsdom has none; the recorded changes' table measures itself with one
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
        vi.useRealTimers();
        vi.unstubAllGlobals();
    });

    it('shows a detection still being checked, with the end of the check, and answers it at once', async () => {
        serve([change({check_until: until})]);
        render(InboxPage, {notices});
        const pill = await screen.findByTestId('checking');
        // the pill keeps the confidence pill's box; the time is in its tooltip and in the text
        expect(pill.className).toContain('bat-pill');
        expect(pill.textContent).toBe('checking');
        expect(pill.getAttribute('title')).toMatch(/^still being checked until /);
        expect(screen.getByText(/^Still being checked until .*You can answer now/)).toBeTruthy();
        expect(calls.some((c) => c.url.includes('status=candidate,pending'))).toBe(true);
        await fireEvent.click(screen.getByRole('button', {name: 'No'}));
        await waitFor(() => expect(calls.some((c) => c.method === 'PATCH')).toBe(true));
        const p = calls.find((c) => c.method === 'PATCH')!;
        expect(p.url).toMatch(/api\/v1\/changes\/1$/);
        expect(JSON.parse(p.body!)).toEqual({status: 'rejected'});
    });

    it('says when the window has passed and the next reading decides', async () => {
        serve([change({check_until: until - 7_200_000})]);
        render(InboxPage, {notices});
        await screen.findByTestId('checking');
        expect(screen.getByText(/^Still being checked: the first voltage the device reports after /)).toBeTruthy();
    });

    it('shows a detection that passed the check with its confidence, as before', async () => {
        serve([change({status: 'pending', confidence: 'high'})]);
        render(InboxPage, {notices});
        await screen.findByText('likely');
        expect(screen.queryByTestId('checking')).toBeNull();
        expect(screen.queryByText(/Still being checked/)).toBeNull();
    });
});
