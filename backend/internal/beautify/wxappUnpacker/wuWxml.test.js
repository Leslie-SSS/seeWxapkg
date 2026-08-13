const assert = require('node:assert/strict');
const test = require('node:test');
const {parseScript, staticEvaluate} = require('./wuStatic.js');
const {_internals} = require('./wuWxml.js');

const {collectTopLevelFunctions, evaluateRegistryNode, extractWxmlRegistries, stripLeadingBraceStatement} = _internals;

// A WeChat 4.x-style page-frame bundle: wxs modules are bare top-level
// function declarations (np_%d) referenced by name from the nnm require-map
// and the f_ registry. The pre-fix code skipped the declarations and failed
// the whole extraction with "Unknown identifier: np_0".
function fourXBundle() {
    return [
        'function np_0(){var nv_module={nv_exports:{}};nv_module.nv_exports=({nv_msg:"hello"});return nv_module.nv_exports;}',
        'function np_1(){var nv_module={nv_exports:{}};nv_module.nv_exports=({nv_tag:"x"});return nv_module.nv_exports;}',
        'var nv_require=function(){var nnm={"p_utils/format.wxs":np_0,"p_utils/tag.wxs":np_1};var path="pages/index/index";return function(p){return nnm[p]}}()',
        'var d_={};',
        'd_["pages/index/index"]="pages/index/index.wxml";',
        'd_["pages/detail/index"]="pages/detail/index.wxml";',
        'var f_={};',
        'f_["utils/format.wxs"]=np_0;',
        'f_["utils/tag.wxs"]=np_1;',
        'var e_={};',
        'e_["pages/index/index"]=function(e,t,n){var v=e[0];n._n("view","",{});n._o(1)};',
        'if(path&&e_[path]){e_[path].call(null)}'
    ].join('\n');
}

test('extractWxmlRegistries registers np_%d declarations and resolves references', () => {
    const {env, registries} = extractWxmlRegistries(fourXBundle());
    assert.ok(env.np_0, 'np_0 must be registered from the function declaration');
    assert.ok(env.np_1, 'np_1 must be registered from the function declaration');
    assert.equal(typeof env.np_0.toString, 'function');
    assert.match(env.np_0.toString(), /function np_0\(\)/);

    assert.equal(registries.e_['pages/index/index'].__staticFunction, true, 'e_ renderer must be captured');
    assert.equal(registries.d_['pages/index/index'], 'pages/index/index.wxml');
    assert.equal(registries.f_['utils/format.wxs'].__staticFunction, true, 'f_ entry referencing np_0 must resolve');
});

test('nnm require-map referencing np_%d evaluates without crashing', () => {
    const {env} = extractWxmlRegistries(fourXBundle());
    // This is the exact expression that previously threw
    // "Unknown identifier: np_0" and failed the whole registry extraction.
    const wrapped = parseScript('({"p_utils/format.wxs":np_0,"p_utils/tag.wxs":np_1})');
    const requireInfo = evaluateRegistryNode(wrapped.body[0].expression, '{"p_utils/format.wxs":np_0}', env);
    assert.equal(typeof requireInfo['p_utils/format.wxs'].toString, 'function');
    assert.match(requireInfo['p_utils/format.wxs'].toString(), /function np_0\(\)/);
});

test('extractWxmlRegistries tolerates the trailing path dispatcher', () => {
    const code = [
        'var d_={};',
        'd_["a"]="a.wxml";',
        'var e_={"a":function(e,t,n){}};',
        'if(path&&e_[path]){e_[path].call(null)}'
    ].join('\n');
    const {registries} = extractWxmlRegistries(code);
    assert.ok(registries.e_.a, 'dispatcher must not fail extraction');
});

test('the nnm data block is stripped before registry parsing', () => {
    // When the nv_require IIFE invocation marker is unavailable, doFrame
    // strips the leading `{...}` require-map literal (unparseable at statement
    // position) before walking the registries.
    const code = [
        '{"p_utils/format.wxs":np_0,"p_utils/tag.wxs":np_1};',
        'function np_0(){return {}}',
        'var d_={"a":"a.wxml"};',
        'var e_={"a":function(e,t,n){}};'
    ].join('\n');
    const stripped = stripLeadingBraceStatement(code);
    assert.ok(!stripped.startsWith('{'), 'leading data block must be stripped');
    const {registries} = extractWxmlRegistries(stripped);
    assert.ok(registries.e_.a, 'registry walk must succeed after the strip');
});

test('a dynamic entry in an object-literal registry initializer does not fail extraction', () => {
    // `var e_ = {a: np_0, b: <dynamic>}` — entry "a" must survive even when
    // "b" cannot be statically evaluated; the whole extraction must not throw.
    const code = [
        'function np_0(){return {}}',
        'var d_={};',
        'd_["pages/index/index.wxml"]={};',
        'var e_={"pages/index/index.wxml":{f:function(e,t,n){var a=_v();return a;}, j:runtime_value}};'
    ].join('\n');
    const {registries} = extractWxmlRegistries(code);
    assert.ok(registries.e_['pages/index/index.wxml'].f, 'static entry must survive a dynamic sibling');
});

test('stripLeadingBraceStatement tolerates strings and comments', () => {
    const code = '{"a": "};", "b": 1 /* } */};\nvar d_={"x":"x.wxml"};';
    const stripped = stripLeadingBraceStatement(code);
    assert.ok(stripped.startsWith('var d_'), 'brace counting must respect strings and comments');
});

test('classic f_ = nv_require(...) entries keep working', () => {
    const code = [
        'function np_0(){var nv_module={nv_exports:{}};return nv_module.nv_exports;}',
        'var nv_require=function(){var nnm={"p_a/comm.wxs":np_0};return function(p){return nnm[p]}}()',
        "f_['a/comm.wxs'] = nv_require(\"p_a/comm.wxs\");",
        "f_['b/index.wxml'] = {};"
    ].join('\n');
    const {registries} = extractWxmlRegistries(code);
    const entry = registries.f_['a/comm.wxs'];
    assert.ok(entry && entry.__staticFunction, 'nv_require call must remain a static function entry');
    assert.equal(entry.returnValue, 'p_a/comm.wxs');
});

test('dynamic registry entries degrade gracefully instead of failing extraction', () => {
    const code = [
        'var d_={};',
        'd_["a"]="a.wxml";',
        'var e_={"a":not_declared_anywhere};',
        'e_["b"]={f:function(e,t,n){var a=_v();return a;}};'
    ].join('\n');
    const {registries} = extractWxmlRegistries(code);
    // The unresolvable "a" entry is skipped with a warning; the static "b"
    // entry and the d_ registry must survive.
    assert.ok(registries.e_.b, 'static member entries must survive dynamic siblings');
    assert.equal(registries.d_.a, 'a.wxml');
});

test('np_%d declarations before the nv_require bootstrap survive the doFrame slice', () => {
    // Some compiler layouts declare wxs modules BEFORE the nv_require
    // bootstrap that doFrame slices away. collectTopLevelFunctions runs
    // against the unsliced bundle; the walk receives its env as a baseline.
    const code = [
        '(function(z){var a=11;function Z(ops){z.push(ops)}',
        "Z([3,'index']);",
        "Z([3,'hello']);",
        '})(z);__WXML_GLOBAL__.ops_set.$gwx=z;',
        'function np_0(){var nv_module={nv_exports:{}};return nv_module.nv_exports;}',
        "var nv_require=function(){var nnm={'p_utils/msg.wxs':np_0};var path='';return function(p){return nnm[p]}}()",
        'var d_={};',
        'd_["pages/index/index"]={};',
        'var f_={};',
        'f_["utils/msg.wxs"]=np_0;',
        'var e_={};',
        'e_["pages/index/index"]={f:function(e,t,n){var a=_v();var b=_o(1);_(a,b);return a;}};',
        'if(path&&e_[path]){e_[path].call(null)}'
    ].join('\n');

    // Replicate the doFrame slice: nv_require bootstrap + dispatcher removed.
    const before = '\nvar nv_require=function(){var nnm=';
    const beforeIdx = code.lastIndexOf(before);
    let sliced = code.slice(beforeIdx + before.length);
    const pathIdx = sliced.lastIndexOf('if(path&&e_[path]){');
    if (pathIdx !== -1) sliced = sliced.slice(0, pathIdx);
    const endOfRequire = sliced.search(/\(\)(?:\r?\n)/);
    if (endOfRequire !== -1) {
        const newline = sliced.indexOf('\n', endOfRequire);
        sliced = sliced.slice(newline + 1);
    }

    const preEnv = collectTopLevelFunctions(code);
    assert.ok(preEnv.np_0, 'pre-registration must capture declarations outside the slice');
    const {env, registries} = extractWxmlRegistries(sliced, preEnv);
    assert.ok(env.np_0, 'walk must inherit the pre-registered functions');
    assert.equal(registries.e_['pages/index/index'].f.__staticFunction, true);
});

test('staticEvaluate resolves registered functions from env', () => {
    const {env} = extractWxmlRegistries(fourXBundle());
    assert.equal(staticEvaluate({type: 'Identifier', name: 'np_0'}, env), env.np_0);
});
