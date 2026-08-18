const assert = require('node:assert/strict');
const test = require('node:test');
const {mkdtempSync, writeFileSync, rmSync} = require('node:fs');
const {tmpdir} = require('node:os');
const {join} = require('node:path');

const {parseWXSS} = require('./verify_artifacts.js');

test('4.x wxcs runtime directives are stripped before CSS parsing', () => {
  const dir = mkdtempSync(join(tmpdir(), 'wxcs-'));
  const file = join(dir, 'comp.wxss');
  writeFileSync(file, [
    '.mask {',
    '    padding: 48rpx;',
    '    ;wxcs_style_padding: 48rpx;',
    '    position: fixed;',
    '    wxcs_originclass: .mask;',
    '    wxcs_fileinfo: ./components/mask;',
    '}'
  ].join('\n'));
  try {
    parseWXSS(file); // must not throw (strict parser mode)
  } finally {
    rmSync(dir, {recursive: true, force: true});
  }
});

test('plain CSS still parses unchanged', () => {
  const dir = mkdtempSync(join(tmpdir(), 'wxcs2-'));
  const file = join(dir, 'plain.css');
  writeFileSync(file, '.a { color: red; background: #fff; }\n.a .b { margin: 0 auto; }\n');
  try {
    parseWXSS(file);
  } finally {
    rmSync(dir, {recursive: true, force: true});
  }
});
