#!/usr/bin/env node

import { createHash } from "node:crypto";
import { mkdir, writeFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";

const HEADER_LENGTH = 14;
const UINT32_MAX = 0xffff_ffff;

const FIXTURE_FILES = [
  ["app.js", "App({onLaunch(){}});\n"],
  [
    "app.json",
    '{"pages":["pages/index/index"],"window":{"navigationBarTitleText":"CI Smoke"}}\n',
  ],
  ["app.wxss", "page{background:#fff}\n"],
  ["pages/index/index.js", 'Page({data:{message:"compose-smoke"}});\n'],
  ["pages/index/index.json", '{"navigationBarTitleText":"Smoke"}\n'],
  ["pages/index/index.wxml", '<view class="page">{{message}}</view>\n'],
  ["pages/index/index.wxss", ".page{padding:24rpx;color:#123456}\n"],
];

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

function assertUint32(value, label) {
  assert(Number.isSafeInteger(value) && value >= 0 && value <= UINT32_MAX, `${label} exceeds uint32`);
}

function normalizeEntries(files) {
  const entries = files.map(([name, content]) => {
    assert(typeof name === "string" && name.length > 0, "fixture entry name must be non-empty");
    assert(!name.startsWith("/") && !name.includes("\\") && !name.split("/").includes(".."), `unsafe fixture entry: ${name}`);
    return {
      name,
      nameBytes: Buffer.from(name, "utf8"),
      content: Buffer.from(content, "utf8"),
    };
  });
  entries.sort((left, right) => (left.name < right.name ? -1 : left.name > right.name ? 1 : 0));
  assert(new Set(entries.map(({ name }) => name)).size === entries.length, "duplicate fixture entry");
  return entries;
}

function buildWxapkg(files) {
  const entries = normalizeEntries(files);
  const indexLength = entries.reduce((size, entry) => size + 12 + entry.nameBytes.length, 4);
  const bodyLength = entries.reduce((size, entry) => size + entry.content.length, 0);
  assertUint32(indexLength, "index length");
  assertUint32(bodyLength, "body length");

  const index = Buffer.alloc(indexLength);
  index.writeUInt32BE(entries.length, 0);
  let indexCursor = 4;
  let bodyOffset = HEADER_LENGTH + indexLength;

  for (const entry of entries) {
    assertUint32(entry.nameBytes.length, `name length for ${entry.name}`);
    assertUint32(entry.content.length, `content length for ${entry.name}`);
    assertUint32(bodyOffset, `body offset for ${entry.name}`);
    index.writeUInt32BE(entry.nameBytes.length, indexCursor);
    indexCursor += 4;
    entry.nameBytes.copy(index, indexCursor);
    indexCursor += entry.nameBytes.length;
    index.writeUInt32BE(bodyOffset, indexCursor);
    indexCursor += 4;
    index.writeUInt32BE(entry.content.length, indexCursor);
    indexCursor += 4;
    bodyOffset += entry.content.length;
  }

  assert(indexCursor === index.length, "fixture index length mismatch");
  assertUint32(bodyOffset, "package length");

  const header = Buffer.alloc(HEADER_LENGTH);
  header[0] = 0xbe;
  header.writeUInt32BE(0, 1);
  header.writeUInt32BE(index.length, 5);
  header.writeUInt32BE(bodyLength, 9);
  header[13] = 0xed;

  const data = Buffer.concat([header, index, ...entries.map(({ content }) => content)]);
  verifyWxapkg(data, entries);
  return data;
}

function verifyWxapkg(data, expectedEntries) {
  assert(data.length >= HEADER_LENGTH + 4, "generated package is too small");
  assert(data[0] === 0xbe && data[13] === 0xed, "generated package markers are invalid");

  const indexLength = data.readUInt32BE(5);
  const bodyLength = data.readUInt32BE(9);
  const indexEnd = HEADER_LENGTH + indexLength;
  assert(data.length === indexEnd + bodyLength, "generated package header lengths are inconsistent");

  let cursor = HEADER_LENGTH;
  const fileCount = data.readUInt32BE(cursor);
  cursor += 4;
  assert(fileCount === expectedEntries.length, "generated package file count is inconsistent");

  let expectedOffset = indexEnd;
  for (const expected of expectedEntries) {
    const nameLength = data.readUInt32BE(cursor);
    cursor += 4;
    assert(cursor + nameLength + 8 <= indexEnd, `generated index entry is truncated: ${expected.name}`);
    const name = data.subarray(cursor, cursor + nameLength).toString("utf8");
    cursor += nameLength;
    const offset = data.readUInt32BE(cursor);
    cursor += 4;
    const size = data.readUInt32BE(cursor);
    cursor += 4;

    assert(name === expected.name, `generated index order mismatch: ${name}`);
    assert(offset === expectedOffset, `generated body offset mismatch: ${name}`);
    assert(size === expected.content.length, `generated body size mismatch: ${name}`);
    assert(data.subarray(offset, offset + size).equals(expected.content), `generated body content mismatch: ${name}`);
    expectedOffset += size;
  }

  assert(cursor === indexEnd, "generated package has trailing index bytes");
  assert(expectedOffset === data.length, "generated package has trailing body bytes");
}

async function main() {
  const args = process.argv.slice(2);
  if (args.length !== 1) {
    throw new Error("usage: node scripts/generate_compose_smoke_fixture.mjs <output.wxapkg>");
  }

  const outputPath = resolve(args[0]);
  assert(extname(outputPath).toLowerCase() === ".wxapkg", "output path must end in .wxapkg");

  const data = buildWxapkg(FIXTURE_FILES);
  await mkdir(dirname(outputPath), { recursive: true });
  await writeFile(outputPath, data, { mode: 0o600 });

  const digest = createHash("sha256").update(data).digest("hex");
  process.stdout.write(`generated ${outputPath} (${data.length} bytes, sha256=${digest})\n`);
}

main().catch((error) => {
  process.stderr.write(`fixture generation failed: ${error.message}\n`);
  process.exitCode = 1;
});
