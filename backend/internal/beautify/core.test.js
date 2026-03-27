const assert = require('node:assert/strict');
const test = require('node:test');

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
  assert.match(result.content, /\/\* Page Definition \*\//);
  assert.match(result.content, /\/\* Page loaded - receive options from navigation \*\//);
  assert.match(result.content, /onLoad\(options\)/);
  assert.match(result.content, /const page = this;/);
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
  assert.match(result.content, /onShareAppMessage\(res\)/);
  assert.match(result.content, /\.map\(\(item, index\) => item \+ index\)/);
  assert.match(result.content, /\.then\(function \(res\)/);
  assert.match(result.content, /\.catch\(function \(err\)/);
  assert.match(result.content, /success\(res\)/);
  assert.match(result.content, /fail\(err\)/);
});

test('beautifyHTML formats WXML with stable indentation', async () => {
  const input = '<view><text wx:if="{{a}}">1</text><custom-card data-id="{{id}}"><text>2</text></custom-card><text wx:else>3</text></view>';
  const result = await beautifyHTML(input);

  assert.equal(result.success, true);
  assert.match(result.content, /<text wx:if="{{a}}">1<\/text>/);
  assert.match(result.content, /<custom-card data-id="{{id}}">/);
  assert.match(result.content, /<text wx:else>3<\/text>/);
});

test('beautifyCSS formats WXSS constructs with prettier', async () => {
  const input = '@import "./base.wxss";.a,.b{width:100rpx;color:var(--primary);padding:calc(100% - 20rpx)}';
  const result = await beautifyCSS(input);

  assert.equal(result.success, true);
  assert.match(result.content, /@import '\.\/base\.wxss';/);
  assert.match(result.content, /\.a,\s*\.b \{/);
  assert.match(result.content, /width: 100rpx;/);
  assert.match(result.content, /padding: calc\(100% - 20rpx\);/);
});

test('beautifyJS falls back to original content with warning for broken syntax', async () => {
  const input = 'const x = ;';
  const result = await beautifyJS(input, 'broken.js', getRuntimeConfig({ deobfuscateEnabled: true }));

  assert.equal(result.success, true);
  assert.equal(result.content.trim(), input);
  assert.ok(result.warning);
});

test('beautifyHTML returns original content with warning when both formatters fail', async () => {
  const input = '\u0000<view>';
  const result = await beautifyHTML(input);

  assert.equal(result.success, true);
  assert.equal(result.content, input);
  assert.ok(result.warning);
});

test('beautifyCSS returns original content with warning when both formatters fail', async () => {
  const input = '\u0000.a{color:red}';
  const result = await beautifyCSS(input);

  assert.equal(result.success, true);
  assert.equal(result.content, input);
  assert.ok(result.warning);
});
