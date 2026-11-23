import {expect, test} from '@playwright/test';

// A self-built device (HB-) keeps its battery voltage on another channel than 0, here as
// BATTERY_VOLTAGE on channel 1: the addon finds it, records it and shows it like any other.

test('a homebrew device: the voltage from channel 1 (BATTERY_VOLTAGE)', async ({page, request}) => {
    const devs = (await (await request.get('api/v1/devices')).json()).devices as {address: string; voltage_dp?: string; voltage?: number}[];
    const iaq = devs.find((d) => d.address === 'HBIAQ0001');
    expect(iaq?.voltage_dp).toBe('1/BATTERY_VOLTAGE');
    expect(iaq?.voltage).toBe(2.95);
    await page.goto('./');
    await page.getByRole('button', {name: 'HBIAQ0001', exact: true}).click();
    const detail = page.getByTestId('device-detail');
    await expect(detail).toBeVisible();
    await expect(detail.getByText('2,95 V').first()).toBeVisible();
    await expect(detail.getByText('Dieses Modell meldet keine Spannung')).toHaveCount(0);
});
