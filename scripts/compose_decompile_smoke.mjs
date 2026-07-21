#!/usr/bin/env node

import { readFile } from "node:fs/promises";
import { basename, resolve } from "node:path";

const TERMINAL_STATUSES = new Set(["completed", "partial", "failed"]);
const REQUIRED_STAGES = [
  "unpacking",
  "recovering_manifest",
  "recovering_js",
  "recovering_wxml",
  "recovering_wxss",
  "formatting",
  "verifying",
  "packaging",
];
const POLL_TIMEOUT_MS = 180_000;
const REQUEST_TIMEOUT_MS = 15_000;

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

function delay(milliseconds) {
  return new Promise((resolveDelay) => setTimeout(resolveDelay, milliseconds));
}

function parseBaseURL(value) {
  const url = new URL(value);
  assert(url.protocol === "http:" || url.protocol === "https:", "base URL must use http or https");
  assert(!url.username && !url.password, "base URL must not contain credentials");
  assert(!url.search && !url.hash, "base URL must not contain a query or fragment");
  url.pathname = url.pathname.replace(/\/*$/, "/");
  return url;
}

function endpoint(baseURL, pathname) {
  return new URL(pathname.replace(/^\/+/, ""), baseURL);
}

async function fetchWithTimeout(url, options = {}) {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS);
  try {
    return await fetch(url, { ...options, signal: controller.signal });
  } finally {
    clearTimeout(timeout);
  }
}

async function requireJSON(response, label) {
  const body = await response.text();
  if (!response.ok) {
    throw new Error(`${label} returned HTTP ${response.status}: ${body.slice(0, 500)}`);
  }
  try {
    return JSON.parse(body);
  } catch {
    throw new Error(`${label} returned invalid JSON: ${body.slice(0, 500)}`);
  }
}

function decodeASCIIName(bytes) {
  for (const byte of bytes) {
    assert(byte >= 0x20 && byte <= 0x7e, "ZIP contains a non-ASCII entry name");
  }
  return bytes.toString("ascii");
}

function assertSafeSourceEntry(name) {
  assert(name.startsWith("src/"), `ZIP entry is outside src/: ${name}`);
  assert(!name.includes("\\") && !name.includes(":"), `ZIP entry is not cross-platform safe: ${name}`);
  const segments = name.split("/");
  assert(segments.length >= 2 && segments.every((segment) => segment !== "" && segment !== "." && segment !== ".."), `ZIP entry has an unsafe path: ${name}`);
}

function findEndOfCentralDirectory(archive) {
  const minimumOffset = Math.max(0, archive.length - 22 - 0xffff);
  for (let offset = archive.length - 22; offset >= minimumOffset; offset -= 1) {
    if (archive.readUInt32LE(offset) === 0x0605_4b50) {
      const commentLength = archive.readUInt16LE(offset + 20);
      if (offset + 22 + commentLength === archive.length) {
        return offset;
      }
    }
  }
  throw new Error("download is not a valid ZIP archive (EOCD not found)");
}

function readZipEntries(archive) {
  assert(archive.length >= 22, "downloaded ZIP is empty or truncated");
  const eocdOffset = findEndOfCentralDirectory(archive);
  const diskNumber = archive.readUInt16LE(eocdOffset + 4);
  const centralDisk = archive.readUInt16LE(eocdOffset + 6);
  const diskEntries = archive.readUInt16LE(eocdOffset + 8);
  const totalEntries = archive.readUInt16LE(eocdOffset + 10);
  const centralSize = archive.readUInt32LE(eocdOffset + 12);
  const centralOffset = archive.readUInt32LE(eocdOffset + 16);

  assert(diskNumber === 0 && centralDisk === 0 && diskEntries === totalEntries, "multi-disk ZIP archives are not supported");
  assert(totalEntries > 0, "downloaded ZIP contains no files");
  assert(totalEntries !== 0xffff && centralSize !== 0xffff_ffff && centralOffset !== 0xffff_ffff, "unexpected ZIP64 archive");
  assert(centralOffset + centralSize === eocdOffset, "ZIP central directory bounds are inconsistent");

  const entries = [];
  let cursor = centralOffset;
  for (let index = 0; index < totalEntries; index += 1) {
    assert(cursor + 46 <= eocdOffset && archive.readUInt32LE(cursor) === 0x0201_4b50, `invalid ZIP central entry ${index}`);
    const flags = archive.readUInt16LE(cursor + 8);
    const nameLength = archive.readUInt16LE(cursor + 28);
    const extraLength = archive.readUInt16LE(cursor + 30);
    const commentLength = archive.readUInt16LE(cursor + 32);
    const diskStart = archive.readUInt16LE(cursor + 34);
    const localOffset = archive.readUInt32LE(cursor + 42);
    const nextCursor = cursor + 46 + nameLength + extraLength + commentLength;
    assert(nextCursor <= eocdOffset, `truncated ZIP central entry ${index}`);
    assert((flags & 0x1) === 0, "ZIP contains an encrypted entry");
    assert(diskStart === 0 && localOffset !== 0xffff_ffff, "ZIP entry uses unsupported disk or ZIP64 metadata");

    const name = decodeASCIIName(archive.subarray(cursor + 46, cursor + 46 + nameLength));
    assertSafeSourceEntry(name);
    assert(localOffset + 30 <= centralOffset && archive.readUInt32LE(localOffset) === 0x0403_4b50, `invalid ZIP local header: ${name}`);
    const localNameLength = archive.readUInt16LE(localOffset + 26);
    const localExtraLength = archive.readUInt16LE(localOffset + 28);
    assert(localOffset + 30 + localNameLength + localExtraLength <= centralOffset, `truncated ZIP local header: ${name}`);
    const localName = decodeASCIIName(archive.subarray(localOffset + 30, localOffset + 30 + localNameLength));
    assert(localName === name, `ZIP local and central names differ: ${name}`);

    entries.push(name);
    cursor = nextCursor;
  }

  assert(cursor === eocdOffset, "ZIP central directory contains trailing data");
  assert(new Set(entries).size === entries.length, "ZIP contains duplicate entries");
  assert(entries.includes("src/app.json"), "ZIP does not contain src/app.json");
  return entries;
}

function assertSameSet(actual, expected, label) {
  const actualSorted = [...actual].sort();
  const expectedSorted = [...expected].sort();
  assert(JSON.stringify(actualSorted) === JSON.stringify(expectedSorted), `${label} does not match the ZIP entries`);
}

async function submitTask(baseURL, fixturePath) {
  const fixture = await readFile(fixturePath);
  assert(fixture.length >= 18 && fixture[0] === 0xbe && fixture[13] === 0xed, "fixture is not a wxapkg file");

  const form = new FormData();
  form.append("file", new Blob([fixture], { type: "application/octet-stream" }), basename(fixturePath));
  form.append("beautify", "true");
  form.append("decompile", "true");

  const response = await fetchWithTimeout(endpoint(baseURL, "/api/compile"), {
    method: "POST",
    body: form,
  });
  const payload = await requireJSON(response, "compile upload through the frontend");
  assert(payload.success === true, `compile upload was rejected: ${payload.message ?? "unknown error"}`);
  assert(typeof payload.taskId === "string" && /^[a-f0-9-]+$/.test(payload.taskId), "compile response has an invalid taskId");
  return payload.taskId;
}

async function waitForTask(baseURL, taskId) {
  const deadline = Date.now() + POLL_TIMEOUT_MS;
  let lastStatus = "";
  while (Date.now() < deadline) {
    const response = await fetchWithTimeout(endpoint(baseURL, `/api/tasks/${taskId}`));
    const task = await requireJSON(response, "task polling through the frontend");
    assert(task.id === taskId, "task endpoint returned a different task");
    assert(typeof task.status === "string" && task.status.length > 0, "task endpoint omitted status");
    if (task.status !== lastStatus) {
      process.stdout.write(`[compose-smoke] task ${taskId}: ${task.status} (${task.progress ?? "?"}%)\n`);
      lastStatus = task.status;
    }
    if (TERMINAL_STATUSES.has(task.status)) {
      return task;
    }
    await delay(750);
  }
  throw new Error(`task ${taskId} did not reach a terminal state within ${POLL_TIMEOUT_MS / 1000}s`);
}

function assertCompletedTask(task) {
  if (task.status !== "completed") {
    const failedStages = Array.isArray(task.stages)
      ? task.stages.filter((stage) => stage.partial || !stage.success).map((stage) => `${stage.stage}:${stage.status}`)
      : [];
    throw new Error(
      `deterministic fixture ended as ${task.status}; errorCode=${task.errorCode ?? "none"}; error=${task.errorMessage ?? "none"}; stages=${failedStages.join(",") || "none"}`,
    );
  }

  assert(task.progress === 100, `completed task reported progress ${task.progress}`);
  assert(task.score?.verifierPassed === true, "completed task did not pass the artifact verifier");
  assert(Array.isArray(task.stages), "completed task omitted stage results");
  const stages = new Map(task.stages.map((stage) => [stage.stage, stage]));
  for (const requiredStage of REQUIRED_STAGES) {
    const stage = stages.get(requiredStage);
    assert(stage, `completed task omitted stage ${requiredStage}`);
    assert(stage.success === true && stage.partial === false && stage.status === "success", `stage ${requiredStage} did not fully succeed`);
  }

  assert(task.artifacts?.downloadReady === true, "completed task is not downloadable");
  assert(typeof task.artifacts.downloadUrl === "string", "completed task omitted downloadUrl");
  assert(Number.isSafeInteger(task.artifacts.fileCount) && task.artifacts.fileCount > 0, "completed task has an invalid artifact count");
  assert(Array.isArray(task.artifacts.files), "completed task omitted artifact files");
}

async function downloadAndVerify(baseURL, task) {
  const downloadURL = new URL(task.artifacts.downloadUrl, baseURL);
  assert(downloadURL.origin === baseURL.origin, "downloadUrl points to another origin");
  assert(downloadURL.pathname === `/api/download/${task.id}`, "downloadUrl has an unexpected path");

  const response = await fetchWithTimeout(downloadURL);
  if (!response.ok) {
    throw new Error(`artifact download through the frontend returned HTTP ${response.status}`);
  }
  assert((response.headers.get("content-type") ?? "").toLowerCase().includes("application/zip"), "artifact download is not application/zip");
  assert((response.headers.get("content-disposition") ?? "").toLowerCase().includes("attachment"), "artifact download is missing attachment disposition");

  const archive = Buffer.from(await response.arrayBuffer());
  assert(archive.length > 0, "artifact download is empty");
  if (task.artifacts.archiveSize !== undefined) {
    assert(archive.length === task.artifacts.archiveSize, "downloaded size differs from task metadata");
  }
  const contentLength = response.headers.get("content-length");
  if (contentLength !== null) {
    assert(Number(contentLength) === archive.length, "downloaded size differs from Content-Length");
  }

  const zipEntries = readZipEntries(archive);
  assert(zipEntries.length === task.artifacts.fileCount, "ZIP entry count differs from task metadata");
  assertSameSet(
    task.artifacts.files.map((file) => file.path),
    zipEntries,
    "artifact file list",
  );
  return { archiveSize: archive.length, zipEntries };
}

async function main() {
  const args = process.argv.slice(2);
  if (args.length !== 2) {
    throw new Error("usage: node scripts/compose_decompile_smoke.mjs <frontend-base-url> <fixture.wxapkg>");
  }

  const baseURL = parseBaseURL(args[0]);
  const fixturePath = resolve(args[1]);
  const taskId = await submitTask(baseURL, fixturePath);
  const task = await waitForTask(baseURL, taskId);
  assertCompletedTask(task);
  const { archiveSize, zipEntries } = await downloadAndVerify(baseURL, task);
  process.stdout.write(
    `[compose-smoke] passed: task=${taskId}, entries=${zipEntries.length}, archive=${archiveSize} bytes, root=src/\n`,
  );
}

main().catch((error) => {
  process.stderr.write(`[compose-smoke] failed: ${error.message}\n`);
  process.exitCode = 1;
});
