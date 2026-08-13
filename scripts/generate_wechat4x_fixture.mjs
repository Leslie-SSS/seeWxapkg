#!/usr/bin/env node
// Builds a synthetic WeChat-4.x-style wxapkg fixture: the main package ships
// page-frame.js (wxs modules as bare np_%d function declarations referenced by
// the nnm require-map) instead of page-frame.html/app-wxss.js. This exercises
// the wuWxml np_%d registry fix and the wuWxapkg page-frame.js main-source fix.
import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";

const HEADER_LENGTH = 14;
const UINT32_MAX = 0xffff_ffff;

const PAGE_FRAME_JS = [
    // z-table (classic opcode builder shape)
    "(function(z){var a=11;function Z(ops){z.push(ops)}",
    "Z([3,'index']);",
    "Z([3,'hello']);",
    "})(z);__WXML_GLOBAL__.ops_set.$gwx=z;",
    // wxs module as a bare function declaration (4.x shape)
    "function np_0(){var nv_module={nv_exports:{}};nv_module.nv_exports=({nv_msg:'hello'});return nv_module.nv_exports;}",
    // nnm require-map referencing np_0 by bare identifier (pre-fix: Unknown identifier)
    "var nv_require=function(){var nnm={'p_utils/msg.wxs':np_0};var path='pages/index/index';return function(p){return nnm[p]}}()",
    "var d_={};",
    'd_["pages/index/index.wxml"]={};',
    "var f_={};",
    'f_["utils/msg.wxs"]=np_0;',
    "var e_={};",
    'e_["pages/index/index.wxml"]={f:function(e,t,n){',
    "var a=_v();",
    'var b=_n("view","");',
    "var c=_o(1);",
    '_(a,b);',
    '_(b,c);',
    "return a;",
    "}};",
    "if(path&&e_[path]){e_[path].call(null)}",
].join("\n");

const FILES = [
    ["app.js", "App({onLaunch(){}});\n"],
    ['app.json', '{"pages":["pages/index/index"],"window":{"navigationBarTitleText":"4x synthetic"}}\n'],
    ["app.wxss", "page{background:#fff}\n"],
    ["app-config.json", '{"pages":["pages/index/index"],"window":{"navigationBarTitleText":"4x synthetic"}}\n'],
    ["app-service.js", "App({onLaunch(){}});\nPage({data:{message:'4x'}});\n"],
    ["page-frame.js", PAGE_FRAME_JS],
    ["pages/index/index.js", "Page({data:{message:'4x'}});\n"],
];

function assertUint32(value, label) {
    if (!Number.isSafeInteger(value) || value < 0 || value > UINT32_MAX) {
        throw new Error(`${label} exceeds uint32: ${value}`);
    }
}

const entries = FILES.map(([name, content]) => ({
    name,
    nameBytes: Buffer.from(name, "utf8"),
    content: Buffer.from(content, "utf8"),
}));
entries.sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));

const indexLength = entries.reduce((size, e) => size + 12 + e.nameBytes.length, 4);
const bodyLength = entries.reduce((size, e) => size + e.content.length, 0);
assertUint32(indexLength, "index length");
assertUint32(bodyLength, "body length");

const index = Buffer.alloc(indexLength);
index.writeUInt32BE(entries.length, 0);
let indexCursor = 4;
let bodyOffset = HEADER_LENGTH + indexLength;
for (const e of entries) {
    assertUint32(e.nameBytes.length, `name length ${e.name}`);
    assertUint32(e.content.length, `content length ${e.name}`);
    assertUint32(bodyOffset, `body offset ${e.name}`);
    index.writeUInt32BE(e.nameBytes.length, indexCursor);
    indexCursor += 4;
    e.nameBytes.copy(index, indexCursor);
    indexCursor += e.nameBytes.length;
    index.writeUInt32BE(bodyOffset, indexCursor);
    indexCursor += 4;
    index.writeUInt32BE(e.content.length, indexCursor);
    indexCursor += 4;
    bodyOffset += e.content.length;
}

const header = Buffer.alloc(HEADER_LENGTH);
header[0] = 0xbe;
header.writeUInt32BE(0, 1);
header.writeUInt32BE(indexLength, 5);
header.writeUInt32BE(bodyLength, 9);
header[13] = 0xed;

const out = resolve(process.argv[2] || "/tmp/4x-fixture.wxapkg");
mkdirSync(dirname(out), { recursive: true });
writeFileSync(out, Buffer.concat([header, index, ...entries.map((e) => e.content)]), { mode: 0o600 });
console.log(`generated ${out} (${HEADER_LENGTH + indexLength + bodyLength} bytes)`);
