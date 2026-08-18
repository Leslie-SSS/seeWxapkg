const assert = require('node:assert/strict');
const test = require('node:test');
const {getZ} = require('./wuRestoreZ.js');

function getZAsync(code) {
    return new Promise((resolve, reject) => {
        getZ(code, (z) => resolve(z));
    });
}

test('WeChat 4.x grouped z-tables (gz$gwx_1 shape) are recovered', async () => {
    const code = [
        'function gz$gwx_1(){',
        'if( __WXML_GLOBAL__.ops_cached.$gwx_1)return __WXML_GLOBAL__.ops_cached.$gwx_1',
        '__WXML_GLOBAL__.ops_cached.$gwx_1=[];',
        '(function(z){var a=11;function Z(ops){z.push(ops)}',
        "Z([3,'hello']);",
        "Z([3,'world']);",
        '})(__WXML_GLOBAL__.ops_cached.$gwx_1);return __WXML_GLOBAL__.ops_cached.$gwx_1',
        '}',
        'function gz$gwx_2(){',
        'if( __WXML_GLOBAL__.ops_cached.$gwx_2)return __WXML_GLOBAL__.ops_cached.$gwx_2',
        '__WXML_GLOBAL__.ops_cached.$gwx_2=[];',
        '(function(z){var a=11;function Z(ops){z.push(ops)}',
        "Z([3,'foo']);",
        'Z(z[0]);',
        '})(__WXML_GLOBAL__.ops_cached.$gwx_2);return __WXML_GLOBAL__.ops_cached.$gwx_2',
        '}'
    ].join('\n');
    const z = await getZAsync(code);
    assert.ok(z.mul, 'grouped tables must produce z.mul');
    assert.deepEqual(Object.keys(z.mul).sort(), ['_1', '_2']);
    assert.deepEqual(z.mul._1, ['hello', 'world']);
    assert.deepEqual(z.mul._2, ['foo', 'foo']);
});

test('classic single z-table still works', async () => {
    const code = [
        '(function(z){var a=11;function Z(ops){z.push(ops)}',
        "Z([3,'classic']);",
        '})(z);__WXML_GLOBAL__.ops_set.$gwx=z;'
    ].join('\n');
    const z = await getZAsync(code);
    assert.ok(!z.mul, 'single table must not be grouped');
    assert.deepEqual(z, ['classic']);
});
