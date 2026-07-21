const assert = require('node:assert/strict');
const test = require('node:test');

const diagnostics = require('./wxappUnpacker/wuDiagnostics.js');
const {
  collectStaticZ,
  isUnresolvedValue,
  restoreAll,
} = require('./wxappUnpacker/wuRestoreZ.js');
const { _internals } = require('./wxappUnpacker/wuWxml.js');
const { parseScript } = require('./wxappUnpacker/wuStatic.js');

test('modern WXML opcodes resolve a=11 and ordered Z(z[n]) references statically', () => {
  const code = [
    '(function(z){var a=11;function Z(ops){z.push(ops)}',
    "Z([3,'prefix'])",
    "Z([a,[3,'hello '],[[7],[3,'name']]])",
    'Z(z[1])',
    '})(z);',
  ].join(';');

  const opcodes = collectStaticZ(code);
  assert.equal(opcodes.length, 3);
  assert.equal(opcodes[1][0], 11);
  assert.strictEqual(opcodes[2], opcodes[1]);

  const restored = restoreAll(opcodes);
  assert.deepEqual(restored, ['prefix', 'hello {{name}}', 'hello {{name}}']);
});

test('opcode collection ignores nested, dead-branch, shadowed, and post-return Z calls', () => {
  const opcodes = collectStaticZ([
    '(function(z){var a=11;function Z(ops){z.push(ops)}',
    "function decoy(){Z([3,'nested'])}",
    "function shadowed(Z){Z([3,'shadowed'])}",
    "if(false){Z([3,'dead'])}",
    "Z([3,'real'])",
    'return',
    "Z([3,'after-return'])",
    '})(z);',
  ].join(';'));

  assert.deepEqual(restoreAll(opcodes), ['real']);
});

test('static opcode recovery never executes calls and preserves unknown table slots', () => {
  let executed = 0;
  globalThis.__seewxUnsafeOpcode = () => {
    executed += 1;
    return [3, 'must-not-run'];
  };
  const originalWarn = console.warn;
  console.warn = () => {};

  try {
    const opcodes = collectStaticZ([
      '(function(z){var a=11;function Z(ops){z.push(ops)}',
      'Z(__seewxUnsafeOpcode())',
      'Z(z[0])',
      "Z([3,'after'])",
      '})(z);',
    ].join(';'));
    const restored = restoreAll(opcodes);

    assert.equal(executed, 0);
    assert.equal(restored.length, 3);
    assert.equal(isUnresolvedValue(restored[0]), true);
    assert.equal(isUnresolvedValue(restored[1]), true);
    assert.match(restored[0].reason, /Unsupported static expression: CallExpression/);
    assert.equal(restored[1].reason, 'depends on an unresolved opcode');
    assert.equal(restored[2], 'after');
  } finally {
    console.warn = originalWarn;
    delete globalThis.__seewxUnsafeOpcode;
  }
});

test('registry extraction ignores nested, dead-branch, and shadowed decoy assignments', () => {
  const extracted = _internals.extractWxmlRegistries([
    "var x=['pages/real/index.wxml']",
    "function decoy(){var fake=function(){return 'fake'};e_['pages/decoy/index.wxml']={f:fake}}",
    "function shadowed(e_){e_['pages/shadowed/index.wxml']={f:function(){return 'shadowed'}}}",
    "if(false){e_['pages/dead/index.wxml']={f:function(){return 'dead'}}}",
    'var renderer=function(){return root}',
    'e_[x[0]]={f:renderer}',
  ].join(';'));

  assert.deepEqual(Object.keys(extracted.registries.e_), ['pages/real/index.wxml']);
  assert.deepEqual(Object.keys(extracted.registries.d_), []);
  assert.equal(extracted.registries.e_['pages/real/index.wxml'].f.toString(), 'function(){return root}');
});

test('source function reads only a certain direct return and ignores nested or dead returns', () => {
  const certainSource = [
    '(function(){',
    "function nested(){return 'nested'}",
    "if(false){return 'dead'}",
    "return 'real'",
    '})',
  ].join(';');
  const certainNode = parseScript(certainSource).body[0].expression;
  const certain = _internals.sourceFunction(certainNode, certainSource, Object.create(null));
  assert.equal(certain.returnValue, 'real');

  const uncertainSource = "(function(){if(flag){return 'conditional'};return 'not-certain'})";
  const uncertainNode = parseScript(uncertainSource).body[0].expression;
  const uncertain = _internals.sourceFunction(uncertainNode, uncertainSource, Object.create(null));
  assert.equal(uncertain.returnValue, undefined);

  const arrowSource = "(()=> 'arrow-value')";
  const arrowNode = parseScript(arrowSource).body[0].expression;
  const arrow = _internals.sourceFunction(arrowNode, arrowSource, Object.create(null));
  assert.equal(arrow.returnValue, 'arrow-value');
});

test('unresolved WXML values become non-rendering markers and one aggregate diagnostic', () => {
  const context = _internals.createWxmlRecoveryContext();
  const output = String(_internals.elemToString({
    tag: 'view',
    v: {
      bindtap: '{{item.id}}',
      catchtap: undefined,
      class: undefined,
      disabled: null,
    },
    son: [{
      tag: '__textNode__',
      textNode: true,
      content: undefined,
    }],
  }, 0, false, context));

  assert.doesNotMatch(output, /Empty/);
  assert.match(output, /<!-- seewx-recovery: unresolved attributes omitted -->/);
  assert.match(output, /<!-- seewx-recovery: unresolved text omitted -->/);
  assert.match(output, /bindtap="\{\{item\.id\}\}"/);
  assert.match(output, / disabled(?:\s|>)/);
  assert.doesNotMatch(output, /catchtap=/);
  assert.doesNotMatch(output, /class=/);
  assert.equal(context.unresolvedAttributeCount, 2);
  assert.equal(context.unresolvedTextCount, 1);
  assert.equal(context.unresolvedEventAttributeCount, 1);

  _internals.flushWxmlRecoveryDiagnostics(context, 'pages/example/index.wxml');
  const report = diagnostics.snapshot();
  const unresolved = report.diagnostics.find(item =>
    item.code === 'fallback.wxml.unresolved_fragments' && item.file === 'pages/example/index.wxml');
  const eventBinding = report.diagnostics.find(item =>
    item.code === 'fallback.wxml.suspicious_event_bindings');

  assert.ok(unresolved);
  assert.deepEqual(unresolved.metadata, {
    count: 3,
    textCount: 1,
    attributeCount: 2,
    eventAttributeCount: 1,
    attributes: { catchtap: 1, class: 1 },
    marker: 'seewx-recovery',
    runtimeSourcePreserved: true,
  });
  assert.equal(eventBinding, undefined);
  assert.doesNotMatch(JSON.stringify(unresolved.metadata), /item\.id|expression|binding/);
});

test('unresolved text marker preserves adjacent known whitespace exactly', () => {
  const context = _internals.createWxmlRecoveryContext();
  const output = String(_internals.elemToString({
    tag: 'text',
    v: {},
    son: [
      { tag: '__textNode__', textNode: true, content: 'left ' },
      { tag: '__textNode__', textNode: true, content: undefined },
      { tag: '__textNode__', textNode: true, content: ' right' },
    ],
  }, 0, false, context));

  assert.match(output, /left <!-- seewx-recovery: unresolved text omitted --> right/);
  assert.equal(context.unresolvedTextCount, 1);
});

test('valid moustache event binding is preserved without a partial diagnostic', () => {
  const context = _internals.createWxmlRecoveryContext();
  const output = String(_internals.elemToString({
    tag: 'view',
    v: { bindtap: '{{ handlerName }}' },
    son: [],
  }, 0, false, context));

  assert.match(output, /bindtap="\{\{ handlerName \}\}"/);
  _internals.flushWxmlRecoveryDiagnostics(context, 'pages/dynamic/index.wxml');
  const report = diagnostics.snapshot();
  assert.equal(report.diagnostics.some(item => item.file === 'pages/dynamic/index.wxml'), false);
});
