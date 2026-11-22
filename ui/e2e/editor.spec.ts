import {expect, test, type APIRequestContext, type Page} from '@playwright/test';

// The device page's battery editor against the real API: per device, per model, the device's own
// values over the model's over the table, each level reset on its own, and a model the table
// lacks (a self-built device). The server's state is shared: every test starts with no overrides.

const API = 'api/v1';
const H = {'X-CCU-Addon-Request': '1'};
const HB = 'HB-UNI-Sen-TEMP-DS18B20';

async function clean(request: APIRequestContext) {
    for (const m of ['HmIP-WRC2', HB]) await request.delete(`${API}/models/${m}/override`, {headers: H});
    for (const [i, a] of [
        ['HmIP-RF', 'WRC2A'],
        ['HmIP-RF', 'WRC2B'],
        ['BidCos-RF', 'HBSEN0001'],
        ['BidCos-RF', 'HBSEN0002'],
    ]) {
        await request.delete(`${API}/devices/${i}/${a}/override`, {headers: H});
    }
}

async function openDevice(page: Page, address: string) {
    await page.goto('./');
    await page.getByRole('button', {name: address, exact: true}).click();
    await expect(page.getByTestId('device-detail')).toBeVisible();
}

async function back(page: Page) {
    await page.getByRole('button', {name: /Geräte/}).first().click();
}

test.beforeEach(async ({request}) => {
    await clean(request);
});
test.afterAll(async ({request}) => {
    await clean(request);
});

test('a model the table lacks: set per device, then reset', async ({page}) => {
    await openDevice(page, 'HBSEN0001');
    await expect(page.getByTestId('slots')).toContainText('unbekannt');
    await expect(page.getByTestId('own-values')).toHaveCount(0);

    await page.getByTestId('edit-batteries').click();
    const ed = page.getByTestId('battery-editor');
    await expect(ed.getByTestId('editor-state')).toContainText('fehlt in der Batterietabelle');
    await ed.getByTestId('editor-power').selectOption('battery');
    await ed.getByTestId('editor-add-slot').click();
    await ed.getByTestId('editor-count-0').fill('2');
    await ed.getByTestId('editor-type-0').selectOption('AA');
    await ed.getByTestId('editor-save').click();
    await expect(ed).toBeHidden();

    await expect(page.getByTestId('slots')).toContainText('2× AA');
    await expect(page.getByTestId('slots-origin')).toHaveText('(eigener Wert dieses Geräts)');
    await expect(page.getByTestId('power')).toContainText('Batterien');
    await expect(page.getByTestId('own-device')).toBeVisible();
    await expect(page.getByTestId('own-model')).toHaveCount(0);
    // the phone's width: nothing scrolls sideways
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);

    await page.getByTestId('reset-device').click();
    await expect(page.getByTestId('slots')).toContainText('unbekannt');
    await expect(page.getByTestId('own-values')).toHaveCount(0);
});

test('a model the table lacks: per model for both devices, one device on top', async ({page}) => {
    await openDevice(page, 'HBSEN0001');
    await page.getByTestId('edit-batteries').click();
    let ed = page.getByTestId('battery-editor');
    await ed.getByText(`alle Geräte des Modells ${HB}`).click();
    await ed.getByTestId('editor-power').selectOption('battery');
    await ed.getByTestId('editor-add-slot').click();
    await ed.getByTestId('editor-count-0').fill('2');
    await ed.getByTestId('editor-type-0').selectOption('AA');
    await ed.getByTestId('editor-save').click();
    await expect(page.getByTestId('slots-origin')).toHaveText(`(eigener Wert für alle ${HB})`);

    // the other device of the model has it too, and gets its own slots on top
    await back(page);
    await openDevice(page, 'HBSEN0002');
    await expect(page.getByTestId('slots')).toContainText('2× AA');
    await expect(page.getByTestId('slots-origin')).toHaveText(`(eigener Wert für alle ${HB})`);
    await page.getByTestId('edit-batteries').click();
    ed = page.getByTestId('battery-editor');
    await expect(ed.getByRole('radio', {name: 'nur dieses Gerät'})).toBeChecked();
    await expect(ed.getByTestId('editor-type-0')).toHaveValue('AA');
    await ed.getByTestId('editor-count-0').fill('1');
    await ed.getByTestId('editor-type-0').selectOption('CR2032');
    await ed.getByTestId('editor-save').click();
    await expect(page.getByTestId('slots')).toContainText('1× CR2032');
    await expect(page.getByTestId('slots-origin')).toHaveText('(eigener Wert dieses Geräts)');
    await expect(page.getByTestId('power-origin')).toHaveText(`(eigener Wert für alle ${HB})`);
    await expect(page.getByTestId('own-model')).toBeVisible();
    await expect(page.getByTestId('own-device')).toBeVisible();

    // the model's reset leaves the device's own slots
    await page.getByTestId('reset-model').click();
    await expect(page.getByTestId('own-model')).toHaveCount(0);
    await expect(page.getByTestId('slots')).toContainText('1× CR2032');
    await expect(page.getByTestId('slots-origin')).toHaveText('(eigener Wert dieses Geräts)');
    await back(page);
    await openDevice(page, 'HBSEN0001');
    await expect(page.getByTestId('slots')).toContainText('unbekannt');
});

test('table < model < device for a model the table has', async ({page}) => {
    await openDevice(page, 'WRC2A');
    await expect(page.getByTestId('slots')).toContainText('2× AAA');
    await expect(page.getByTestId('slots-origin')).toHaveText('(aus der Batterietabelle)');

    // the model: 2x AA outdoors, for both remotes
    await page.getByTestId('edit-batteries').click();
    let ed = page.getByTestId('battery-editor');
    await expect(ed.getByTestId('editor-state')).toContainText('Batterietabelle');
    await ed.getByText('alle Geräte des Modells HmIP-WRC2').click();
    await ed.getByTestId('editor-type-0').selectOption('AA');
    await ed.getByTestId('editor-outdoor').check();
    await ed.getByTestId('editor-save').click();
    await expect(page.getByTestId('slots')).toContainText('2× AA');
    await expect(page.getByTestId('slots-origin')).toHaveText('(eigener Wert für alle HmIP-WRC2)');
    await expect(page.getByTestId('outdoor-origin')).toHaveText('(eigener Wert für alle HmIP-WRC2)');

    // the second remote: rechargeables of its own, the model's slots and outdoor stay
    await back(page);
    await openDevice(page, 'WRC2B');
    await expect(page.getByTestId('slots')).toContainText('2× AA');
    await page.getByTestId('edit-batteries').click();
    ed = page.getByTestId('battery-editor');
    await expect(ed.getByRole('radio', {name: 'nur dieses Gerät'})).toBeChecked();
    await ed.getByTestId('editor-rechargeable').check();
    await ed.getByTestId('editor-save').click();
    await expect(page.getByTestId('chemistry')).toContainText('NiMH');
    await expect(page.getByTestId('chemistry-origin')).toHaveText('(eigener Wert dieses Geräts)');
    await expect(page.getByTestId('slots-origin')).toHaveText('(eigener Wert für alle HmIP-WRC2)');
    // what the API stored: only what differs from the model
    const dev = await page.request.get(`${API}/devices/HmIP-RF/WRC2B/override`);
    expect((await dev.json()).override).toEqual({rechargeable: true});

    // the model's editor says that the device's own values come first
    await page.getByTestId('edit-batteries').click();
    ed = page.getByTestId('battery-editor');
    await ed.getByText('alle Geräte des Modells HmIP-WRC2').click();
    await expect(ed.getByTestId('editor-state')).toContainText('Die eigenen Werte dieses Geräts gehen vor.');
    await ed.getByRole('button', {name: 'Abbrechen'}).click();

    // reset the device: back to the model's; reset the model: back to the table
    await page.getByTestId('reset-device').click();
    await expect(page.getByTestId('chemistry')).toHaveCount(0);
    await expect(page.getByTestId('slots-origin')).toHaveText('(eigener Wert für alle HmIP-WRC2)');
    await page.getByTestId('reset-model').click();
    await expect(page.getByTestId('slots')).toContainText('2× AAA');
    await expect(page.getByTestId('slots-origin')).toHaveText('(aus der Batterietabelle)');
    await expect(page.getByTestId('outdoor')).toHaveCount(0);
});

test('saving the table values removes the override; reset in the editor', async ({page, request}) => {
    await openDevice(page, 'WRC2A');
    await page.getByTestId('edit-batteries').click();
    let ed = page.getByTestId('battery-editor');
    await expect(ed.getByTestId('editor-reset')).toBeDisabled();
    await ed.getByTestId('editor-outdoor').check();
    await ed.getByTestId('editor-save').click();
    await expect(page.getByTestId('own-device')).toBeVisible();
    expect((await (await request.get(`${API}/devices/HmIP-RF/WRC2A/override`)).json()).override).toEqual({outdoor: true});

    await page.getByTestId('edit-batteries').click();
    ed = page.getByTestId('battery-editor');
    await ed.getByTestId('editor-outdoor').uncheck();
    await ed.getByTestId('editor-save').click();
    await expect(page.getByTestId('own-values')).toHaveCount(0);
    expect((await (await request.get(`${API}/devices/HmIP-RF/WRC2A/override`)).json()).override).toBeNull();

    await page.getByTestId('edit-batteries').click();
    ed = page.getByTestId('battery-editor');
    await ed.getByTestId('editor-power').selectOption('external');
    await expect(ed.getByTestId('editor-slots')).toHaveCount(0);
    await ed.getByTestId('editor-save').click();
    // a power supply unit: the page offers the supply switch, set to it
    await expect(page.getByTestId('supply')).toHaveValue('external');
    await expect(page.getByTestId('own-device')).toBeVisible();
    await page.getByTestId('edit-batteries').click();
    ed = page.getByTestId('battery-editor');
    await ed.getByTestId('editor-reset').click();
    await expect(ed).toBeHidden();
    await expect(page.getByTestId('power')).toContainText('Batterien');
    await expect(page.getByTestId('power-origin')).toHaveText('(aus der Batterietabelle)');
});
