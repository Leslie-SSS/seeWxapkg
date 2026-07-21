const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');
const csstree = require('css-tree');

const wu = require('./wxappUnpacker/wuLib.js');
const diagnostics = require('./wxappUnpacker/wuDiagnostics.js');
const {doWxss, _internals} = require('./wxappUnpacker/wuWxss.js');

function runWxss(directory) {
  return new Promise(resolve => {
    doWxss(directory, () => wu.addIO(resolve));
  });
}

function listFiles(directory, extension, output = []) {
  for (const entry of fs.readdirSync(directory, {withFileTypes: true})) {
    const filename = path.join(directory, entry.name);
    if (entry.isDirectory()) listFiles(filename, extension, output);
    else if (entry.isFile() && filename.endsWith(extension)) output.push(filename);
  }
  return output;
}

test('4.x shared stylesheet extraction ignores dynamic, dead, nested, and shadowed decoys', () => {
  let executed = 0;
  globalThis.__seewxWxssProbe = () => {
    executed += 1;
    return ['.executed{display:block}'];
  };

  try {
    const table = _internals.extractStaticStylesheetData([
      'globalThis.__seewxWxssProbe();',
      'var shared = shared || {};',
      "if (false) shared['./dead.wxss']=['.dead{}'];",
      "if (unknownFlag) shared['./unknown.wxss']=['.unknown{}'];",
      "shared['./dynamic.wxss']=globalThis.__seewxWxssProbe();",
      "function nested(){shared['./nested.wxss']=['.nested{}'];var _C=shared}",
      "{let shared={};shared['./block-shadowed.wxss']=['.block-shadowed{}'];}",
      "if (!shared.hasOwnProperty('./real.wxss')) shared['./real.wxss']=['.',[1],'real{color:red}'];",
      'var setCssToHead = function(){',
      '  var _C = shared;',
      "  function shadowed(){var _C={'./shadowed.wxss':['.shadowed{}']};}",
      "  if(false){var _C={'./branch.wxss':['.branch{}']};}",
      '  return _C;',
      '};',
    ].join('\n'));

    assert.equal(executed, 0);
    assert.deepEqual(Object.keys(table), ['./real.wxss']);
    assert.deepEqual(table['./real.wxss'], ['.', [1], 'real{color:red}']);
    assert.equal(Object.prototype.hasOwnProperty.call(table, './dead.wxss'), false);
    assert.equal(Object.prototype.hasOwnProperty.call(table, './dynamic.wxss'), false);
    assert.equal(Object.prototype.hasOwnProperty.call(table, './nested.wxss'), false);
    assert.equal(Object.prototype.hasOwnProperty.call(table, './shadowed.wxss'), false);
    assert.equal(Object.prototype.hasOwnProperty.call(table, './block-shadowed.wxss'), false);
  } finally {
    delete globalThis.__seewxWxssProbe;
  }
});

test('string stylesheet keys produce deterministic traversal-safe base filenames', () => {
  assert.equal(_internals.baseStylesheetName(12), '12.wxss');
  assert.equal(_internals.baseStylesheetName('12'), '12.wxss');
  const filename = _internals.baseStylesheetName('./styles/order-common.wxss');
  assert.match(filename, /^shared-[a-f0-9]{16}\.wxss$/);
  assert.equal(filename, _internals.baseStylesheetName('./styles/order-common.wxss'));
  assert.equal(path.basename(filename), filename);
  assert.doesNotMatch(filename.slice(0, -'.wxss'.length), /[/\\.]/);
});

test('4.x shared WXSS is recovered without path selectors, execution, parser failures, or import recursion', async () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'seewx-wxss-4x.'));
  let executed = 0;
  globalThis.__seewxWxssProbe = () => {
    executed += 1;
  };

  const commonKey = './styles/order-common.wxss';
  const selfKey = './styles/self-cycle.wxss';
  const cycleAKey = './styles/cycle-a.wxss';
  const cycleBKey = './styles/cycle-b.wxss';
  const frame = [
    '<!doctype html><html><body><script>',
    'window.__wcc_version__=4;',
    'globalThis.__seewxWxssProbe();',
    'var __COMMON_STYLESHEETS__=__COMMON_STYLESHEETS__||{};',
    `if(!__COMMON_STYLESHEETS__.hasOwnProperty(${JSON.stringify(commonKey)}))__COMMON_STYLESHEETS__[${JSON.stringify(commonKey)}]=[".",[1],"confirm-page{color:#c00}"];`,
    `if(!__COMMON_STYLESHEETS__.hasOwnProperty(${JSON.stringify(selfKey)}))__COMMON_STYLESHEETS__[${JSON.stringify(selfKey)}]=[[2,${JSON.stringify(selfKey)}]];`,
    `if(!__COMMON_STYLESHEETS__.hasOwnProperty(${JSON.stringify(cycleAKey)}))__COMMON_STYLESHEETS__[${JSON.stringify(cycleAKey)}]=[[2,${JSON.stringify(cycleBKey)}]];`,
    `if(!__COMMON_STYLESHEETS__.hasOwnProperty(${JSON.stringify(cycleBKey)}))__COMMON_STYLESHEETS__[${JSON.stringify(cycleBKey)}]=[[2,${JSON.stringify(cycleAKey)}]];`,
    'var setCssToHead = function(file, _xcInvalid, info){var _C=__COMMON_STYLESHEETS__;return file;};',
    'var __wxAppCode__={};',
    `__wxAppCode__['pages/a/index.wxss']=setCssToHead([[2,${JSON.stringify(commonKey)}],".",[1],"local-a{margin:",[0,2],"}"]);`,
    `__wxAppCode__['pages/b/index.wxss']=setCssToHead([[2,${JSON.stringify(commonKey)}],".",[1],"local-b{padding:",[0,4],"}"]);`,
    `__wxAppCode__['pages/missing/index.wxss']=setCssToHead([[2,"./styles/missing.wxss"],".",[1],"missing-page{display:block}"]);`,
    `__wxAppCode__['pages/self/index.wxss']=setCssToHead([[2,${JSON.stringify(selfKey)}]]);`,
    `__wxAppCode__['pages/two/index.wxss']=setCssToHead([[2,${JSON.stringify(cycleAKey)}]]);`,
    '</script></body></html>',
  ].join('\n');
  fs.writeFileSync(path.join(directory, 'page-frame.html'), frame);

  const originalLog = console.log;
  const originalWarn = console.warn;
  console.log = () => {};
  console.warn = () => {};
  try {
    await runWxss(directory);

    assert.equal(executed, 0);
    const wxssFiles = listFiles(directory, '.wxss');
    const baseFiles = wxssFiles.filter(filename => filename.includes(`${path.sep}__wuBaseWxss__${path.sep}`));
    assert.equal(baseFiles.length, 1, JSON.stringify(wxssFiles));

    const baseCss = fs.readFileSync(baseFiles[0], 'utf8');
    const pageA = fs.readFileSync(path.join(directory, 'pages/a/index.wxss'), 'utf8');
    const pageB = fs.readFileSync(path.join(directory, 'pages/b/index.wxss'), 'utf8');
    const missing = fs.readFileSync(path.join(directory, 'pages/missing/index.wxss'), 'utf8');
    const selfCycle = fs.readFileSync(path.join(directory, 'pages/self/index.wxss'), 'utf8');
    const twoNodeCycle = fs.readFileSync(path.join(directory, 'pages/two/index.wxss'), 'utf8');
    const allCss = wxssFiles.map(filename => fs.readFileSync(filename, 'utf8')).join('\n');

    assert.match(baseCss, /\.confirm-page\s*\{/);
    assert.match(pageA, /@import/);
    assert.match(pageA, /\.local-a\s*\{/);
    assert.match(pageB, /@import/);
    assert.match(pageB, /\.local-b\s*\{/);
    assert.match(missing, /Unresolved static WXSS import omitted/);
    assert.match(missing, /\.missing-page\s*\{/);
    assert.match(selfCycle, /Cyclic static WXSS import omitted/);
    assert.match(twoNodeCycle, /Cyclic static WXSS import omitted/);
    assert.doesNotMatch(allCss, /\.\/styles\/[^\s{]+\s*\{/);

    for (const filename of wxssFiles) {
      assert.doesNotThrow(() => csstree.parse(fs.readFileSync(filename, 'utf8')), filename);
    }

    const report = diagnostics.snapshot();
    assert.equal(report.diagnostics.some(item => item.code === 'fallback.wxss.data_parse_failed'), false);
    assert.ok(report.diagnostics.some(item => item.code === 'fallback.wxss.unresolved_import'));
    assert.ok(report.diagnostics.some(item => item.code === 'fallback.wxss.import_cycle'));
  } finally {
    console.log = originalLog;
    console.warn = originalWarn;
    delete globalThis.__seewxWxssProbe;
    fs.rmSync(directory, {recursive: true, force: true});
  }
});
