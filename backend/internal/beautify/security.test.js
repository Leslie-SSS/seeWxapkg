const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');

const {
  extractDefineModules,
  safeResolve,
  staticEvaluate,
} = require('./wxappUnpacker/wuStatic.js');
const {
  LIMITS,
  genList,
  header,
  saveFile,
} = require('./wxappUnpacker/wuWxapkg.js');
const wu = require('./wxappUnpacker/wuLib.js');

const unpackerDir = path.resolve(__dirname, 'wxappUnpacker');

function javascriptFiles(root) {
  const files = [];
  for (const entry of fs.readdirSync(root, { withFileTypes: true })) {
    if (entry.name === 'node_modules') continue;
    const fullPath = path.join(root, entry.name);
    if (entry.isDirectory()) files.push(...javascriptFiles(fullPath));
    else if (entry.isFile() && entry.name.endsWith('.js')) files.push(fullPath);
  }
  return files;
}

test('legacy fallback contains no dynamic code execution primitive', () => {
  const forbidden = [
    /require\s*\(\s*['"]vm2['"]\s*\)/,
    /\bVM\s*\.\s*run\s*\(/,
    /\bnew\s+VM\s*\(/,
    /\beval\s*\(/,
    /\bnew\s+Function\s*\(/,
  ];

  for (const filename of javascriptFiles(unpackerDir)) {
    const source = fs.readFileSync(filename, 'utf8');
    for (const pattern of forbidden) {
      assert.doesNotMatch(source, pattern, `${path.basename(filename)} uses ${pattern}`);
    }
  }

  const manifest = fs.readFileSync(path.join(unpackerDir, 'package.json'), 'utf8');
  const lockfile = fs.readFileSync(path.join(unpackerDir, 'package-lock.json'), 'utf8');
  assert.doesNotMatch(manifest, /"vm2"/);
  assert.doesNotMatch(lockfile, /"vm2"|node_modules\/vm2/);
});

test('fallback writes keep recovered source private', { skip: process.platform === 'win32' }, async () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'seewx-private-output.'));
  const directory = path.join(root, 'pages');
  const destination = path.join(directory, 'index.js');
  try {
    fs.mkdirSync(directory, { mode: 0o755 });
    fs.writeFileSync(destination, 'old', { mode: 0o644 });
    wu.save(destination, 'Page({})');
    await new Promise(resolve => wu.addIO(resolve));

    assert.equal(fs.statSync(directory).mode & 0o777, 0o700);
    assert.equal(fs.statSync(destination).mode & 0o777, 0o600);
    assert.equal(fs.readFileSync(destination, 'utf8'), 'Page({})');
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

function packageBuffer(entries, mutations = {}) {
  const indexSize = 4 + entries.reduce((total, entry) => total + 12 + Buffer.byteLength(entry.name), 0);
  const dataStart = 14 + indexSize;
  const data = Buffer.concat(entries.map(entry => Buffer.from(entry.content || '')));
  const index = Buffer.alloc(indexSize);
  index.writeUInt32BE(mutations.fileCount ?? entries.length, 0);
  let cursor = 4;
  let dataOffset = dataStart;
  entries.forEach((entry, entryIndex) => {
    const name = Buffer.from(entry.name);
    index.writeUInt32BE(entry.nameLength ?? name.length, cursor);
    cursor += 4;
    name.copy(index, cursor);
    cursor += name.length;
    index.writeUInt32BE(entry.offset ?? dataOffset, cursor);
    cursor += 4;
    index.writeUInt32BE(entry.size ?? Buffer.byteLength(entry.content || ''), cursor);
    cursor += 4;
    dataOffset += Buffer.byteLength(entry.content || '');
  });

  const packageHeader = Buffer.alloc(14);
  packageHeader.writeUInt8(0xbe, 0);
  packageHeader.writeUInt32BE(0, 1);
  packageHeader.writeUInt32BE(mutations.indexSize ?? index.length, 5);
  packageHeader.writeUInt32BE(mutations.dataLength ?? data.length, 9);
  packageHeader.writeUInt8(0xed, 13);
  return Buffer.concat([packageHeader, index, data]);
}

function parsePackage(buffer) {
  const parsedHeader = header(buffer);
  const index = buffer.subarray(14, 14 + parsedHeader.infoListLength);
  return genList(index, parsedHeader.dataStart, parsedHeader.dataEnd);
}

test('wxapkg index accepts a bounded valid package and writes inside its root', () => {
  const buffer = packageBuffer([{ name: '/pages/index.js', content: 'Page({})' }]);
  const entries = parsePackage(buffer);
  const output = fs.mkdtempSync('/tmp/seewx-unpack-test.');
  try {
    saveFile(output, buffer, entries);
    assert.equal(fs.readFileSync(path.join(output, 'pages/index.js'), 'utf8'), 'Page({})');
  } finally {
    fs.rmSync(output, { recursive: true, force: true });
  }
});

test('wxapkg index rejects traversal, offset overflow, and impossible file counts', () => {
  assert.throws(
    () => parsePackage(packageBuffer([{ name: '../escape.js', content: 'x' }])),
    /Unsafe output path/
  );
  assert.throws(
    () => parsePackage(packageBuffer([{ name: 'safe.js', content: 'x', offset: 0xffffffff, size: 2 }])),
    /data range is outside package data/
  );

  const impossibleIndex = Buffer.alloc(4);
  impossibleIndex.writeUInt32BE(LIMITS.maxFileCount + 1, 0);
  assert.throws(
    () => genList(impossibleIndex, 18, 18),
    /file count .* exceeds/
  );
});

test('wxapkg header rejects truncated and inconsistent declared sizes', () => {
  assert.throws(() => header(Buffer.alloc(13)), /truncated header/);
  const inconsistent = packageBuffer([{ name: 'safe.js', content: 'x' }], { dataLength: 2 });
  assert.throws(() => header(inconsistent), /declared data length extends beyond package/);
});

test('static define extraction never runs module or top-level package code', () => {
  delete global.__seewxStaticProbe;
  const source = [
    'global.__seewxStaticProbe = "top-level";',
    'define("pages/index.js", function(require, module, exports) {',
    '  global.__seewxStaticProbe = "factory";',
    '  module.exports = { ok: true };',
    '});',
  ].join('\n');

  const modules = extractDefineModules(source);
  assert.equal(global.__seewxStaticProbe, undefined);
  assert.equal(modules.length, 1);
  assert.equal(modules[0].name, 'pages/index.js');
  assert.match(modules[0].body, /module\.exports/);
});

test('static evaluator rejects calls and prototype access', () => {
  assert.throws(() => staticEvaluate({ type: 'CallExpression' }), /Unsupported static expression/);
  assert.throws(
    () => safeResolve('/tmp/safe-root', '../escape.js'),
    /Unsafe output path/
  );
});
