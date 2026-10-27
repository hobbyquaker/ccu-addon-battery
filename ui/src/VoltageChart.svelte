<script lang="ts">
    // The voltage curve of one device over its whole record, on uPlot: the live and seeded samples
    // as a line, imported ones muted, the low-battery phases as bands, the accepted changes as
    // markers, the detector's fresh and low levels as thin lines. Colours come from the kit's
    // tokens, read when the chart is (re)built, so dark mode follows the theme.
    import uPlot from 'uplot';
    import 'uplot/dist/uPlot.min.css';
    import {i18n, t} from 'occulite-ui/i18n';
    import {theme} from 'occulite-ui/theme';

    import {timeAxisFor} from './chartTime.js';
    import {formatVoltage, type Change} from './model.js';

    export interface SamplePoint {
        ts: number;
        voltage: number | null;
        low_bat: boolean | null;
        source: string;
    }

    let {
        samples,
        changes = [],
        fresh = undefined,
        low = undefined,
        height = 220,
    }: {
        samples: SamplePoint[];
        changes?: Pick<Change, 'at' | 'kind' | 'status'>[];
        fresh?: number | undefined;
        low?: number | undefined;
        height?: number;
    } = $props();

    let el: HTMLDivElement | undefined = $state();
    let plot: uPlot | undefined;

    function token(name: string, fallback: string): string {
        if (!el) return fallback;
        return getComputedStyle(el).getPropertyValue(name).trim() || fallback;
    }

    function build(): void {
        if (!el) return;
        plot?.destroy();
        plot = undefined;
        const voltages = samples.filter((s) => s.voltage !== null);
        if (voltages.length === 0) return;
        const xs = voltages.map((s) => s.ts / 1000);
        const live: (number | null)[] = voltages.map((s) => (s.source === 'import' ? null : s.voltage));
        const imported: (number | null)[] = voltages.map((s) => (s.source === 'import' ? s.voltage : null));
        // an imported series that ends where the live one begins joins at that point
        const firstLive = live.findIndex((v) => v !== null);
        if (firstLive > 0 && imported[firstLive] === null) imported[firstLive] = live[firstLive] ?? null;
        const fg = token('--hmm-fg', '#222');
        const muted = token('--hmm-fg-muted', '#888');
        const border = token('--hmm-border', '#8884');
        const accent = token('--hmm-accent', '#2c6fb3');
        const warn = token('--hmm-warn', '#b7791f');
        const ok = token('--hmm-ok', '#2a8a3e');
        const lang = i18n.language;
        const time = timeAxisFor(lang);
        const marks = changes.filter((c) => c.status === 'accepted').map((c) => c.at / 1000);
        const lowPhases: [number, number][] = [];
        let start: number | null = null;
        for (const s of samples) {
            if (s.low_bat === true && start === null) start = s.ts / 1000;
            if (s.low_bat === false && start !== null) {
                lowPhases.push([start, s.ts / 1000]);
                start = null;
            }
        }
        const last = samples[samples.length - 1];
        if (start !== null && last) lowPhases.push([start, last.ts / 1000]);

        const opts: uPlot.Options = {
            width: el.clientWidth || 600,
            height,
            cursor: {drag: {x: true, y: false}},
            legend: {show: false},
            scales: {x: {time: true}},
            // German: 14:15, 27.10.26; English keeps uPlot's own labels
            ...(time ? {fmtDate: (tpl: string) => uPlot.fmtDate(tpl, time.names)} : {}),
            axes: [
                {stroke: muted, grid: {stroke: border, width: 1}, ticks: {stroke: border}, ...(time ? {values: time.stamps as uPlot.Axis.Values} : {})},
                {
                    stroke: muted,
                    grid: {stroke: border, width: 1},
                    ticks: {stroke: border},
                    size: 56,
                    values: (_u, vals) => vals.map((v) => formatVoltage(v, lang)),
                },
            ],
            series: [
                {},
                {label: t('chart.live'), stroke: accent, width: 1.5, spanGaps: false, points: {show: voltages.length < 60, size: 4}},
                {label: t('chart.imported'), stroke: muted, width: 1, dash: [4, 3], spanGaps: false, points: {show: false}},
            ],
            hooks: {
                drawClear: [
                    (u) => {
                        const ctx = u.ctx;
                        ctx.save();
                        // the low-battery phases as bands
                        ctx.fillStyle = warn;
                        ctx.globalAlpha = 0.22;
                        for (const [a, b] of lowPhases) {
                            const x0 = u.valToPos(a, 'x', true);
                            const x1 = u.valToPos(b, 'x', true);
                            ctx.fillRect(x0, u.bbox.top, Math.max(2, x1 - x0), u.bbox.height);
                        }
                        ctx.globalAlpha = 1;
                        // the levels
                        ctx.lineWidth = 1;
                        ctx.setLineDash([2, 4]);
                        for (const [lvl, col] of [
                            [fresh, ok],
                            [low, warn],
                        ] as const) {
                            if (lvl === undefined) continue;
                            const y = u.valToPos(lvl, 'y', true);
                            if (y < u.bbox.top || y > u.bbox.top + u.bbox.height) continue;
                            ctx.strokeStyle = col;
                            ctx.beginPath();
                            ctx.moveTo(u.bbox.left, y);
                            ctx.lineTo(u.bbox.left + u.bbox.width, y);
                            ctx.stroke();
                        }
                        ctx.restore();
                    },
                ],
                draw: [
                    (u) => {
                        const ctx = u.ctx;
                        ctx.save();
                        ctx.strokeStyle = fg;
                        ctx.fillStyle = fg;
                        ctx.lineWidth = 1.5;
                        ctx.setLineDash([]);
                        for (const m of marks) {
                            const x = u.valToPos(m, 'x', true);
                            if (x < u.bbox.left || x > u.bbox.left + u.bbox.width) continue;
                            ctx.beginPath();
                            ctx.moveTo(x, u.bbox.top);
                            ctx.lineTo(x, u.bbox.top + u.bbox.height);
                            ctx.stroke();
                            ctx.beginPath();
                            ctx.moveTo(x - 5, u.bbox.top);
                            ctx.lineTo(x + 5, u.bbox.top);
                            ctx.lineTo(x, u.bbox.top + 7);
                            ctx.closePath();
                            ctx.fill();
                        }
                        ctx.restore();
                    },
                ],
            },
        };
        plot = new uPlot(opts, [xs, live, imported], el);
    }

    $effect(() => {
        void samples;
        void changes;
        void fresh;
        void low;
        void i18n.language;
        void theme.choice;
        build();
        return () => {
            plot?.destroy();
            plot = undefined;
        };
    });

    $effect(() => {
        if (!el) return;
        const ro = new ResizeObserver(() => {
            if (plot && el) plot.setSize({width: el.clientWidth, height});
        });
        ro.observe(el);
        return () => ro.disconnect();
    });

    const hasVoltage = $derived(samples.some((s) => s.voltage !== null));
</script>

<div class="bat-chart" bind:this={el} data-testid="voltage-chart">
    {#if !hasVoltage}
        <p class="bat-empty">{t('chart.none')}</p>
    {/if}
</div>
<p class="bat-muted bat-chart-legend">
    <span class="bat-legend-line"></span> {t('chart.live')} · <span class="bat-legend-line dashed"></span> {t('chart.imported')} · <span class="bat-legend-band"></span> {t('chart.low')} · <span class="bat-legend-mark">▾</span> {t('chart.change')}
</p>

<style>
    .bat-chart {
        width: 100%;
        min-height: 60px;
    }

    .bat-chart-legend {
        font-size: 0.85em;
        margin: 4px 0 0;
    }

    .bat-legend-line {
        display: inline-block;
        width: 18px;
        border-top: 2px solid var(--hmm-accent, #2c6fb3);
        vertical-align: middle;
    }

    .bat-legend-line.dashed {
        border-top: 1px dashed var(--hmm-fg-muted, #888);
    }

    .bat-legend-band {
        display: inline-block;
        width: 14px;
        height: 10px;
        background: var(--hmm-warn, #b7791f);
        opacity: 0.3;
        vertical-align: middle;
    }
</style>
