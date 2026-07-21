const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const wu = require('./wxappUnpacker/wuLib.js');
const {doConfig} = require('./wxappUnpacker/wuConfig.js');
const {resolveWxmlOutput} = require('./wxappUnpacker/wuWxml.js');
const {resolveWxssOutput} = require('./wxappUnpacker/wuWxss.js');
const diagnostics = require('./wxappUnpacker/wuDiagnostics.js');

function runConfig(configFile) {
  return new Promise(resolve => {
    doConfig(configFile, () => wu.addIO(resolve));
  });
}

test('malicious app config paths are skipped and never write outside the package root', async () => {
  const testRoot = fs.mkdtempSync('/tmp/seewx-config-paths.');
  const packageRoot = path.join(testRoot, 'package');
  fs.mkdirSync(packageRoot);
  const configFile = path.join(packageRoot, 'app-config.json');
  const config = {
    entryPagePath: 'pages/home.html',
    pages: ['pages/home.html', '../main-escape.html'],
    page: {
      'pages/home.html': {
        window: {
          usingComponents: {
            unsafe: '../../../component-escape',
          },
        },
      },
      '../page-config-escape.html': { window: { navigationBarTitleText: 'unsafe' } },
    },
    subPackages: [
      { root: '../subpackage-root-escape', pages: ['index.html'] },
      { root: 'safe-subpackage', pages: ['../../subpackage-page-escape.html', 'pages/ok.html'] },
    ],
  };
  fs.writeFileSync(configFile, JSON.stringify(config));

  try {
    await runConfig(configFile);

    assert.equal(fs.existsSync(path.join(testRoot, 'main-escape.json')), false);
    assert.equal(fs.existsSync(path.join(testRoot, 'page-config-escape.json')), false);
    assert.equal(fs.existsSync(path.join(testRoot, 'component-escape.json')), false);
    assert.equal(fs.existsSync(path.join(testRoot, 'subpackage-root-escape')), false);
    assert.equal(fs.existsSync(path.join(testRoot, 'subpackage-page-escape.js')), false);

    assert.equal(fs.existsSync(path.join(packageRoot, 'pages/home.json')), true);
    assert.equal(fs.existsSync(path.join(packageRoot, 'safe-subpackage/pages/ok.js')), true);
    const app = JSON.parse(fs.readFileSync(path.join(packageRoot, 'app.json'), 'utf8'));
    assert.deepEqual(app.pages, ['pages/home']);
    assert.equal(app.subPackages.length, 1);
    assert.equal(app.subPackages[0].root, 'safe-subpackage/');
    assert.deepEqual(app.subPackages[0].pages, ['pages/ok']);

    const report = diagnostics.snapshot();
    assert.equal(report.status, 'partial');
    assert.ok(report.diagnostics.some(item => item.code === 'fallback.config.page_config_unsafe'));
    assert.ok(report.diagnostics.some(item => item.code === 'fallback.config.subpackage_root_unsafe'));
  } finally {
    fs.rmSync(testRoot, { recursive: true, force: true });
  }
});

test('WXML and WXSS package-derived outputs reject traversal before writing', () => {
  const testRoot = fs.mkdtempSync('/tmp/seewx-render-paths.');
  const packageRoot = path.join(testRoot, 'package');
  fs.mkdirSync(packageRoot);
  try {
    const unsafeWxml = resolveWxmlOutput(packageRoot, '../wxml-escape.wxml', 'registry');
    const unsafeWxss = resolveWxssOutput(packageRoot, '../wxss-escape.wxss', 'registry');
    assert.equal(unsafeWxml, null);
    assert.equal(unsafeWxss, null);
    assert.equal(fs.existsSync(path.join(testRoot, 'wxml-escape.wxml')), false);
    assert.equal(fs.existsSync(path.join(testRoot, 'wxss-escape.wxss')), false);

    assert.equal(
      resolveWxmlOutput(packageRoot, '/pages/index.wxml', 'registry'),
      path.join(packageRoot, 'pages/index.wxml')
    );
    assert.equal(
      resolveWxssOutput(packageRoot, 'pages/index.wxss', 'registry'),
      path.join(packageRoot, 'pages/index.wxss')
    );
  } finally {
    fs.rmSync(testRoot, { recursive: true, force: true });
  }
});

test('fallback writers never pass package content directly to path.resolve', () => {
  for (const filename of ['wuConfig.js', 'wuWxml.js', 'wuWxss.js']) {
    const source = fs.readFileSync(path.join(__dirname, 'wxappUnpacker', filename), 'utf8');
    assert.doesNotMatch(source, /wu\.save\s*\(\s*path\.resolve\s*\(/);
  }
});
