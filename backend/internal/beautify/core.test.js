const assert = require('node:assert/strict');
const test = require('node:test');
const babelParser = require('@babel/parser');

const {
  beautifyCSS,
  beautifyHTML,
  beautifyJS,
  getRuntimeConfig,
} = require('./core');

test('beautifyJS applies WeChat comments and conservative renames', async () => {
  const input = 'Page({data:{list:[]},onLoad:function(e){var t=this;e.a.forEach(function(e){console.log(e)})}})';
  const result = await beautifyJS(input, 'index.js', getRuntimeConfig({ deobfuscateEnabled: true }));

  assert.equal(result.success, true);
  assert.equal(result.status, 'formatted');
  assert.match(result.content, /\/\* Page Definition \*\//);
  assert.match(result.content, /\/\* Page loaded - receive options from navigation \*\//);
  assert.match(result.content, /onLoad: function \(options\)/);
  assert.match(result.content, /var page = this;/);
  assert.match(result.content, /forEach\(function \(item\)/);
});

test('beautifyJS renames lifecycle, array, promise, and wx callback params conservatively', async () => {
  const input = [
    'Page({',
    '  onShareAppMessage:function(e){return e.from},',
    '  onLoad:function(){',
    '    [1].map((e,n)=>e+n);',
    '    Promise.resolve(1).then(function(e){return e}).catch(function(t){return t});',
    '    wx.request({success:function(e){return e.data},fail:function(t){return t.errMsg}});',
    '  }',
    '})',
  ].join('');

  const result = await beautifyJS(input, 'index.js', getRuntimeConfig({ deobfuscateEnabled: true }));

  assert.equal(result.success, true);
  assert.match(result.content, /onShareAppMessage: function \(res\)/);
  assert.match(result.content, /\.map\(\(item, index\) => item \+ index\)/);
  assert.match(result.content, /\.then\(function \(res\)/);
  assert.match(result.content, /\.catch\(function \(err\)/);
  assert.match(result.content, /success: function \(res\)/);
  assert.match(result.content, /fail: function \(err\)/);
});

test('beautifyJS defaults to format-only without semantic transforms', async () => {
  const input = 'Page({onLoad:function(e){var t=this;return e.a+t.data}})';
  const result = await beautifyJS(input, 'index.js', getRuntimeConfig({ deobfuscateEnabled: false }));

  assert.equal(result.status, 'formatted');
  assert.doesNotMatch(result.content, /Page Definition/);
  assert.match(result.content, /onLoad: function \(e\)/);
  assert.match(result.content, /var t = this;/);
});

test('optional readability pass preserves function-property and var semantics', async () => {
  const input = 'Page({Ctor:function(){},onLoad:function(){if(true){void t}var t=this;return t}})';
  const result = await beautifyJS(input, 'index.js', getRuntimeConfig({ deobfuscateEnabled: true }));
  const ast = babelParser.parse(result.content);
  const pageObject = ast.program.body[0].expression.arguments[0];
  const ctor = pageObject.properties.find(property => property.key.name === 'Ctor');
  const onLoad = pageObject.properties.find(property => property.key.name === 'onLoad');
  const declaration = onLoad.value.body.body.find(statement => statement.type === 'VariableDeclaration');

  assert.equal(ctor.type, 'ObjectProperty');
  assert.equal(ctor.value.type, 'FunctionExpression');
  assert.equal(declaration.kind, 'var');
});

test('beautifyHTML formats WXML with stable indentation', async () => {
  const input = '<view><text wx:if="{{a}}">1</text><custom-card data-id="{{id}}"><text>2</text></custom-card><text wx:else>3</text></view>';
  const result = await beautifyHTML(input);

  assert.equal(result.success, true);
  assert.equal(result.status, 'unchanged');
  assert.match(result.content, /<text wx:if="{{a}}">1<\/text>/);
  assert.match(result.content, /<custom-card data-id="{{id}}">/);
  assert.match(result.content, /<text wx:else>3<\/text>/);
});

test('beautifyHTML preserves text and inline WXS bytes while formatting tag syntax', async () => {
  const input = '<view   class="card"   data-title="{{ title }}">  A\n B <text> C  D </text><wxs module="m">if (a < b) return "<x>";</wxs></view>';
  const result = await beautifyHTML(input);

  assert.equal(result.status, 'formatted');
  assert.match(result.content, /^<view class="card" data-title="{{ title }}">/);
  assert.ok(result.content.includes('  A\n B '));
  assert.ok(result.content.includes('<text> C  D </text>'));
  assert.ok(result.content.includes('<wxs module="m">if (a < b) return "<x>";</wxs>'));
});

test('beautifyCSS formats WXSS constructs with prettier', async () => {
  const input = '@import "./base.wxss";.a,.b{width:100rpx;color:var(--primary);padding:calc(100% - 20rpx)}';
  const result = await beautifyCSS(input);

  assert.equal(result.success, true);
  assert.equal(result.status, 'formatted');
  assert.match(result.content, /@import '\.\/base\.wxss';/);
  assert.match(result.content, /\.a,\s*\.b \{/);
  assert.match(result.content, /width: 100rpx;/);
  assert.match(result.content, /padding: calc\(100% - 20rpx\);/);
});

test('beautifyJS falls back to original content with warning for broken syntax', async () => {
  const input = 'const x = ;';
  const result = await beautifyJS(input, 'broken.js', getRuntimeConfig({ deobfuscateEnabled: true }));

  assert.equal(result.success, true);
  assert.equal(result.status, 'failed');
  assert.equal(result.content.trim(), input);
  assert.ok(result.warning);
});

test('beautifyHTML returns original content with warning when both formatters fail', async () => {
  const input = '\u0000<view>';
  const result = await beautifyHTML(input);

  assert.equal(result.success, true);
  assert.equal(result.status, 'skipped');
  assert.equal(result.content, input);
  assert.ok(result.warning);
});

test('beautifyCSS returns original content with warning when both formatters fail', async () => {
  const input = '\u0000.a{color:red}';
  const result = await beautifyCSS(input);

  assert.equal(result.success, true);
  assert.equal(result.status, 'skipped');
  assert.equal(result.content, input);
  assert.ok(result.warning);
});

test('beautification is idempotent in safe and optional readability modes', async () => {
  const input = 'Page({onLoad:function(e){var t=this;return e.a+t.data}})';
  for (const deobfuscateEnabled of [false, true]) {
    const config = getRuntimeConfig({ deobfuscateEnabled });
    const first = await beautifyJS(input, 'index.js', config);
    const second = await beautifyJS(first.content, 'index.js', config);
    assert.equal(second.content, first.content);
    assert.equal(second.status, 'unchanged');
  }
});
