#!/usr/bin/env node

import { mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import process from "node:process";

const DAY_MS = 24 * 60 * 60 * 1000;
const OUTPUT_FILES = Object.freeze({
  light: "star-history-light.svg",
  dark: "star-history-dark.svg",
});

const THEMES = Object.freeze({
  light: {
    background: "#f8fafc",
    panel: "#ffffff",
    border: "#d7e2df",
    grid: "#dce8e4",
    axis: "#64748b",
    text: "#0f172a",
    muted: "#64748b",
    accent: "#059669",
    accentBright: "#10b981",
    areaStart: "#10b981",
    areaEnd: "#d1fae5",
    glow: "#6ee7b7",
    chip: "#ecfdf5",
    chipBorder: "#a7f3d0",
  },
  dark: {
    background: "#06110e",
    panel: "#0a1713",
    border: "#1b3a31",
    grid: "#17352d",
    axis: "#829c93",
    text: "#ecfdf5",
    muted: "#91aaa1",
    accent: "#34d399",
    accentBright: "#6ee7b7",
    areaStart: "#10b981",
    areaEnd: "#0a1713",
    glow: "#10b981",
    chip: "#0d251e",
    chipBorder: "#1d5b47",
  },
});

function usage() {
  return [
    "Usage:",
    "  node scripts/generate_star_history.mjs \\",
    "    --input <stargazers.json|-> \\",
    "    --output-dir <directory> \\",
    "    --repository <owner/name>",
    "",
    "Use --input - to read from standard input. The JSON may be one API page",
    "or an array of paginated pages.",
  ].join("\n");
}

function parseArguments(argv) {
  const options = {};

  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === "--help" || argument === "-h") {
      process.stdout.write(`${usage()}\n`);
      process.exit(0);
    }

    if (!argument.startsWith("--")) {
      throw new Error(`Unexpected argument: ${argument}`);
    }

    const key = argument.slice(2);
    if (!["input", "output-dir", "repository"].includes(key)) {
      throw new Error(`Unknown option: ${argument}`);
    }
    if (options[key] !== undefined) {
      throw new Error(`Duplicate option: ${argument}`);
    }

    const value = argv[index + 1];
    if (!value || value.startsWith("--")) {
      throw new Error(`Missing value for ${argument}`);
    }
    options[key] = value;
    index += 1;
  }

  for (const required of ["input", "output-dir", "repository"]) {
    if (!options[required]) {
      throw new Error(`Missing required option: --${required}`);
    }
  }

  if (!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(options.repository)) {
    throw new Error("--repository must use the owner/name format");
  }

  return options;
}

function flattenPages(value) {
  if (!Array.isArray(value)) {
    throw new Error("Stargazers input must be a JSON array");
  }

  if (value.every((item) => Array.isArray(item))) {
    return value.flat();
  }
  if (value.some((item) => Array.isArray(item))) {
    throw new Error("Stargazers input cannot mix page arrays and entries");
  }
  return value;
}

function parseStarDates(input) {
  const entries = flattenPages(input);
  const dates = entries.map((entry, index) => {
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) {
      throw new Error(`Stargazer entry ${index} must be an object`);
    }
    if (typeof entry.starred_at !== "string") {
      throw new Error(`Stargazer entry ${index} is missing starred_at`);
    }

    const timestamp = Date.parse(entry.starred_at);
    if (!Number.isFinite(timestamp)) {
      throw new Error(
        `Stargazer entry ${index} has an invalid starred_at value`,
      );
    }
    return new Date(timestamp).toISOString().slice(0, 10);
  });

  return dates.sort();
}

function aggregateByUtcDate(dates) {
  const dailyCounts = new Map();
  for (const date of dates) {
    dailyCounts.set(date, (dailyCounts.get(date) ?? 0) + 1);
  }

  let cumulative = 0;
  return [...dailyCounts.entries()]
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([date, added]) => {
      cumulative += added;
      return { date, added, cumulative };
    });
}

function escapeXml(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&apos;");
}

function formatMonth(date) {
  const [year, month] = date.split("-");
  return `${year}-${month}`;
}

function formatNumber(value) {
  return new Intl.NumberFormat("en-US").format(value);
}

function niceYAxis(total) {
  if (total <= 0) {
    return { maximum: 1, step: 1 };
  }

  const roughStep = Math.max(1, total / 6);
  const magnitude = 10 ** Math.floor(Math.log10(roughStep));
  const normalized = roughStep / magnitude;
  const niceNormalized =
    normalized <= 1 ? 1 : normalized <= 2 ? 2 : normalized <= 5 ? 5 : 10;
  const step = Math.max(1, niceNormalized * magnitude);
  return {
    maximum: Math.ceil(total / step) * step,
    step,
  };
}

function evenlySpacedDates(firstDate, lastDate, desiredCount = 6) {
  const first = Date.parse(`${firstDate}T00:00:00Z`);
  const last = Date.parse(`${lastDate}T00:00:00Z`);
  const daySpan = Math.round((last - first) / DAY_MS);
  if (daySpan <= 0) {
    return [firstDate];
  }

  const count = Math.min(desiredCount, daySpan + 1);
  const ticks = new Set();
  for (let index = 0; index < count; index += 1) {
    const dayOffset = Math.round((daySpan * index) / (count - 1));
    ticks.add(new Date(first + dayOffset * DAY_MS).toISOString().slice(0, 10));
  }
  return [...ticks];
}

function buildChartData(points) {
  if (points.length === 0) {
    return {
      total: 0,
      firstDate: null,
      lastDate: null,
      monthSpan: 0,
      yAxis: niceYAxis(0),
      xTicks: [],
      points: [],
    };
  }

  const firstDate = points[0].date;
  const lastDate = points.at(-1).date;
  const firstMonth =
    Number(firstDate.slice(0, 4)) * 12 + Number(firstDate.slice(5, 7));
  const lastMonth =
    Number(lastDate.slice(0, 4)) * 12 + Number(lastDate.slice(5, 7));

  return {
    total: points.at(-1).cumulative,
    firstDate,
    lastDate,
    monthSpan: lastMonth - firstMonth + 1,
    yAxis: niceYAxis(points.at(-1).cumulative),
    xTicks: evenlySpacedDates(firstDate, lastDate),
    points,
  };
}

function buildStepAfterPath(points) {
  if (points.length === 0) {
    return "";
  }

  const commands = [`M ${points[0].x.toFixed(2)} ${points[0].y.toFixed(2)}`];
  for (const point of points.slice(1)) {
    commands.push(`H ${point.x.toFixed(2)}`, `V ${point.y.toFixed(2)}`);
  }
  return commands.join(" ");
}

function renderSvg(repository, chart, themeName) {
  const theme = THEMES[themeName];
  const width = 900;
  const height = 480;
  const plot = { left: 78, top: 138, width: 760, height: 260 };
  const baseline = plot.top + plot.height;
  const escapedRepository = escapeXml(repository);
  const title = `${escapedRepository} Star growth`;

  const firstDay = chart.firstDate
    ? Date.parse(`${chart.firstDate}T00:00:00Z`)
    : 0;
  const lastDay = chart.lastDate
    ? Date.parse(`${chart.lastDate}T00:00:00Z`)
    : firstDay;
  const daySpan = Math.max(1, Math.round((lastDay - firstDay) / DAY_MS));
  const xForDate = (date) => {
    if (chart.firstDate === chart.lastDate) {
      return plot.left + plot.width / 2;
    }
    const timestamp = Date.parse(`${date}T00:00:00Z`);
    return plot.left + ((timestamp - firstDay) / DAY_MS / daySpan) * plot.width;
  };
  const yForValue = (value) =>
    baseline - (value / chart.yAxis.maximum) * plot.height;

  const plottedPoints = chart.points.map((point) => ({
    ...point,
    x: xForDate(point.date),
    y: yForValue(point.cumulative),
  }));
  const linePath = buildStepAfterPath(plottedPoints);
  const areaPath =
    plottedPoints.length > 0
      ? `${linePath} L ${plottedPoints.at(-1).x.toFixed(2)} ${baseline} L ${plottedPoints[0].x.toFixed(2)} ${baseline} Z`
      : "";

  const yGrid = [];
  for (let value = 0; value <= chart.yAxis.maximum; value += chart.yAxis.step) {
    const y = yForValue(value);
    yGrid.push(
      `    <line x1="${plot.left}" y1="${y.toFixed(2)}" x2="${plot.left + plot.width}" y2="${y.toFixed(2)}" class="grid-line" />`,
    );
    yGrid.push(
      `    <text x="${plot.left - 16}" y="${(y + 4).toFixed(2)}" text-anchor="end" class="axis-label">${formatNumber(value)}</text>`,
    );
  }

  const xGrid = chart.xTicks.flatMap((date, index) => {
    const x = xForDate(date);
    const anchor =
      index === 0
        ? "start"
        : index === chart.xTicks.length - 1
          ? "end"
          : "middle";
    return [
      `    <line x1="${x.toFixed(2)}" y1="${plot.top}" x2="${x.toFixed(2)}" y2="${baseline}" class="grid-line vertical-grid" />`,
      `    <text x="${x.toFixed(2)}" y="${baseline + 34}" text-anchor="${anchor}" class="axis-label">${formatMonth(date)}</text>`,
    ];
  });

  const rangeLabel = chart.firstDate
    ? `${formatMonth(chart.firstDate)} – ${formatMonth(chart.lastDate)}`
    : "Waiting for the first star";
  const updateLabel = chart.lastDate
    ? `Data through ${chart.lastDate} · UTC`
    : "No Stargazers API records yet";
  const description = chart.lastDate
    ? `${escapedRepository} has ${formatNumber(chart.total)} stars from ${chart.firstDate} through ${chart.lastDate}.`
    : `${escapedRepository} has no recorded stars yet.`;

  const emptyState =
    chart.points.length === 0
      ? `
    <g class="empty-state">
      <circle cx="450" cy="268" r="38" fill="${theme.chip}" stroke="${theme.chipBorder}" />
      <path d="M450 243l7.4 15 16.6 2.4-12 11.7 2.8 16.5-14.8-7.8-14.8 7.8 2.8-16.5-12-11.7 16.6-2.4z" fill="${theme.accent}" />
      <text x="450" y="331" text-anchor="middle" class="empty-label">Waiting for the first star</text>
    </g>`
      : "";

  const firstMarker = plottedPoints[0];
  const lastMarker = plottedPoints.at(-1);
  const markers =
    plottedPoints.length > 0
      ? `
    <circle cx="${firstMarker.x.toFixed(2)}" cy="${firstMarker.y.toFixed(2)}" r="4" class="point-marker" />
    <circle cx="${lastMarker.x.toFixed(2)}" cy="${lastMarker.y.toFixed(2)}" r="10" class="point-halo" />
    <circle cx="${lastMarker.x.toFixed(2)}" cy="${lastMarker.y.toFixed(2)}" r="5" class="point-marker" />`
      : "";

  return `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}" role="img" aria-labelledby="chart-title chart-description">
  <title id="chart-title">${title}</title>
  <desc id="chart-description">${description}</desc>
  <defs>
    <linearGradient id="background-gradient" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="${theme.panel}" />
      <stop offset="1" stop-color="${theme.background}" />
    </linearGradient>
    <linearGradient id="area-gradient" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0" stop-color="${theme.areaStart}" stop-opacity="0.30" />
      <stop offset="1" stop-color="${theme.areaEnd}" stop-opacity="0.02" />
    </linearGradient>
    <filter id="line-glow" x="-20%" y="-20%" width="140%" height="140%">
      <feGaussianBlur stdDeviation="3" result="blur" />
      <feMerge>
        <feMergeNode in="blur" />
        <feMergeNode in="SourceGraphic" />
      </feMerge>
    </filter>
    <style>
      text { font-family: Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
      .eyebrow { fill: ${theme.accent}; font-size: 12px; font-weight: 700; letter-spacing: 2.2px; }
      .heading { fill: ${theme.text}; font-size: 24px; font-weight: 720; }
      .stat { fill: ${theme.text}; font-size: 27px; font-weight: 760; }
      .stat-label { fill: ${theme.muted}; font-size: 12px; font-weight: 600; }
      .axis-label { fill: ${theme.axis}; font-size: 11px; font-weight: 520; }
      .footer-label { fill: ${theme.muted}; font-size: 11px; }
      .empty-label { fill: ${theme.muted}; font-size: 13px; font-weight: 600; }
      .grid-line { stroke: ${theme.grid}; stroke-width: 1; vector-effect: non-scaling-stroke; }
      .vertical-grid { stroke-dasharray: 3 7; }
      .trend-line { fill: none; stroke: ${theme.accentBright}; stroke-width: 3; stroke-linecap: round; stroke-linejoin: round; vector-effect: non-scaling-stroke; }
      .trend-glow { fill: none; stroke: ${theme.glow}; stroke-opacity: 0.28; stroke-width: 7; stroke-linecap: round; stroke-linejoin: round; vector-effect: non-scaling-stroke; }
      .point-marker { fill: ${theme.panel}; stroke: ${theme.accentBright}; stroke-width: 3; vector-effect: non-scaling-stroke; }
      .point-halo { fill: ${theme.accentBright}; fill-opacity: 0.13; }
    </style>
  </defs>

  <rect width="${width}" height="${height}" rx="22" fill="url(#background-gradient)" />
  <rect x="0.5" y="0.5" width="${width - 1}" height="${height - 1}" rx="21.5" fill="none" stroke="${theme.border}" />

  <g aria-hidden="true">
    <rect x="48" y="36" width="132" height="28" rx="14" fill="${theme.chip}" stroke="${theme.chipBorder}" />
    <circle cx="65" cy="50" r="4" fill="${theme.accent}" />
    <text x="77" y="54" class="eyebrow">STAR GROWTH</text>
    <text x="48" y="99" class="heading">${escapedRepository}</text>

    <text x="852" y="65" text-anchor="end" class="stat">${formatNumber(chart.total)}</text>
    <text x="852" y="84" text-anchor="end" class="stat-label">TOTAL STARS</text>
    <text x="852" y="104" text-anchor="end" class="stat-label">${escapeXml(rangeLabel)} · ${chart.monthSpan} ${chart.monthSpan === 1 ? "MONTH" : "MONTHS"}</text>
  </g>

  <g aria-hidden="true">
${yGrid.join("\n")}
${xGrid.join("\n")}${
    areaPath
      ? `
    <path d="${areaPath}" fill="url(#area-gradient)" />
    <path d="${linePath}" class="trend-glow" filter="url(#line-glow)" />
    <path d="${linePath}" class="trend-line" />${markers}`
      : ""
  }${emptyState}
  </g>

  <g aria-hidden="true">
    <circle cx="50" cy="446" r="4" fill="${theme.accent}" />
    <text x="62" y="450" class="footer-label">Cumulative stars by UTC date</text>
    <text x="850" y="450" text-anchor="end" class="footer-label">${escapeXml(updateLabel)}</text>
  </g>
</svg>
`;
}

async function writeAtomically(destination, contents) {
  const temporary = `${destination}.tmp-${process.pid}`;
  try {
    await writeFile(temporary, contents, "utf8");
    await rename(temporary, destination);
  } finally {
    await rm(temporary, { force: true });
  }
}

async function readInput(source) {
  if (source !== "-") {
    const inputPath = path.resolve(source);
    return { label: inputPath, contents: await readFile(inputPath, "utf8") };
  }

  const chunks = [];
  for await (const chunk of process.stdin) {
    chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
  }
  return {
    label: "standard input",
    contents: Buffer.concat(chunks).toString("utf8"),
  };
}

async function main() {
  const options = parseArguments(process.argv.slice(2));
  const outputDirectory = path.resolve(options["output-dir"]);
  const source = await readInput(options.input);

  let input;
  try {
    input = JSON.parse(source.contents);
  } catch (error) {
    throw new Error(`Unable to parse ${source.label}: ${error.message}`);
  }

  const dates = parseStarDates(input);
  const chart = buildChartData(aggregateByUtcDate(dates));

  await mkdir(outputDirectory, { recursive: true });
  for (const themeName of Object.keys(OUTPUT_FILES)) {
    const destination = path.join(outputDirectory, OUTPUT_FILES[themeName]);
    await writeAtomically(
      destination,
      renderSvg(options.repository, chart, themeName),
    );
  }

  process.stdout.write(
    `Generated ${Object.values(OUTPUT_FILES).join(" and ")} for ${chart.total} stars through ${chart.lastDate ?? "no data"}.\n`,
  );
}

main().catch((error) => {
  process.stderr.write(`Star history generation failed: ${error.message}\n`);
  process.exitCode = 1;
});
