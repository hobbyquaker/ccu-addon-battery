import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/svelte';
import {addMessages, i18n} from 'occulite-ui/i18n';
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest';

import DevicesPage from './DevicesPage.svelte';
import {MESSAGES} from './messages.js';
import type {DeviceView} from './model.js';

const dev = (p: Partial<DeviceView>): DeviceView => ({
    interface: 'HmIP-RF',
    address: 'X',
    name: '',
    rooms: [],
    model: 'HmIP-WRC2',
    power: 'battery',
    voltage: 2.9,
    low_bat: false,
    unreach: false,
    first_seen: 0,
    ...p,
});

const DEVICES = [
    dev({address: 'A1', name: 'Türe Waschkeller'}),
    dev({address: 'A2', name: 'Fenster Keller', unreach: true}),
    dev({address: 'A3', name: 'Taster Gartenhaus', service_messages_off: true}),
];

function serve(filters: {service_messages: boolean} | undefined) {
    vi.stubGlobal(
        'fetch',
        vi.fn(async (url: string) => {
            const body = String(url).includes('api/v1/devices') ? {devices: DEVICES, filters} : {devices: []};
            return new Response(JSON.stringify(body), {status: 200, headers: {'Content-Type': 'application/json'}});
        }),
    );
}

const notices = {push: vi.fn()} as unknown as import('occulite-ui/notices').NoticesStore;

const names = () =>
    ['Türe Waschkeller', 'Fenster Keller', 'Taster Gartenhaus']
        .filter((n) => screen.queryByText(n))
        .join(',');

describe('DevicesPage: the filter', () => {
    beforeEach(() => {
        addMessages(MESSAGES);
        i18n.language = 'en';
        localStorage.clear();
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

    it('shows every device by default and hides what is ticked, remembered for the next visit', async () => {
        serve({service_messages: true});
        const first = render(DevicesPage, {notices});
        await screen.findByText('Türe Waschkeller');
        expect(names()).toBe('Türe Waschkeller,Fenster Keller,Taster Gartenhaus');
        const unreach = screen.getByTestId('filter-unreach') as HTMLInputElement;
        const serviceOff = screen.getByTestId('filter-service-off') as HTMLInputElement;
        expect(unreach.checked).toBe(false);
        expect(serviceOff.checked).toBe(false);
        expect(serviceOff.disabled).toBe(false);
        // the counts say what each filter would hide
        expect(screen.getByText('Hide unreachable devices (1)')).toBeTruthy();
        expect(screen.getByText('Hide devices with service messages switched off (1)')).toBeTruthy();

        await fireEvent.click(unreach);
        await waitFor(() => expect(names()).toBe('Türe Waschkeller,Taster Gartenhaus'));
        await fireEvent.click(serviceOff);
        await waitFor(() => expect(names()).toBe('Türe Waschkeller'));

        first.unmount();
        render(DevicesPage, {notices});
        await screen.findByText('Türe Waschkeller');
        expect(names()).toBe('Türe Waschkeller');
        expect((screen.getByTestId('filter-unreach') as HTMLInputElement).checked).toBe(true);
        expect((screen.getByTestId('filter-service-off') as HTMLInputElement).checked).toBe(true);
    });

    it('offers only the unreachable filter where the system has no switch for service messages, and says so', async () => {
        // a remembered choice from a CCU hides nothing here
        localStorage.setItem('battery.devices.filter', JSON.stringify({showMains: false, hideUnreach: false, hideServiceOff: true}));
        serve({service_messages: false});
        render(DevicesPage, {notices});
        await screen.findByText('Türe Waschkeller');
        expect(names()).toBe('Türe Waschkeller,Fenster Keller,Taster Gartenhaus');
        const serviceOff = screen.getByTestId('filter-service-off') as HTMLInputElement;
        expect(serviceOff.disabled).toBe(true);
        expect(serviceOff.checked).toBe(false);
        expect(screen.getByText(/only on a CCU3, OpenCCU and piVCCU/)).toBeTruthy();
        await fireEvent.click(screen.getByTestId('filter-unreach'));
        await waitFor(() => expect(names()).toBe('Türe Waschkeller,Taster Gartenhaus'));
    });

    it('hides an ignored device until "Show ignored", and switches a device off and back on (1.5.0)', async () => {
        const list = [dev({address: 'A1', name: 'Türe Waschkeller'}), dev({address: 'A4', name: 'Taster Bad', model: 'ELV-SH-BS2', ignored: true})];
        const calls: string[] = [];
        vi.stubGlobal(
            'fetch',
            vi.fn(async (url: string, init?: RequestInit) => {
                const u = String(url);
                if (init?.method === 'PUT' || init?.method === 'DELETE') {
                    calls.push(`${init.method} ${u.slice(u.indexOf('api/v1/'))}`);
                    const a = list.find((d) => u.includes(`/${d.address}/`));
                    if (a) a.ignored = init.method === 'PUT';
                    return new Response(JSON.stringify({ok: true}), {status: 200, headers: {'Content-Type': 'application/json'}});
                }
                const body = u.includes('api/v1/devices') ? {devices: list.map((d) => ({...d})), filters: {service_messages: false}} : {devices: []};
                return new Response(JSON.stringify(body), {status: 200, headers: {'Content-Type': 'application/json'}});
            }),
        );
        render(DevicesPage, {notices});
        await screen.findByText('Türe Waschkeller');
        expect(screen.queryByText('Taster Bad')).toBeNull();
        expect(screen.getByText('Show ignored (1)')).toBeTruthy();
        await fireEvent.click(screen.getByTestId('filter-ignored'));
        await screen.findByText('Taster Bad');
        expect(screen.getByText('Ignored')).toBeTruthy();
        // back on, then the other one off
        await fireEvent.click(screen.getByText('Monitor again'));
        await waitFor(() => expect(calls).toEqual(['DELETE api/v1/devices/HmIP-RF/A4/ignore']));
        await waitFor(() => expect(screen.getAllByText('Do not monitor').length).toBe(2));
        await fireEvent.click(screen.getAllByText('Do not monitor')[0]!);
        await waitFor(() => expect(calls.length).toBe(2));
        expect(calls[1]).toMatch(/^PUT api\/v1\/devices\/HmIP-RF\/A[14]\/ignore$/);
    });

    it('works against a backend that names no filters (an older one)', async () => {
        serve(undefined);
        render(DevicesPage, {notices});
        await screen.findByText('Türe Waschkeller');
        expect((screen.getByTestId('filter-service-off') as HTMLInputElement).disabled).toBe(true);
    });
});
