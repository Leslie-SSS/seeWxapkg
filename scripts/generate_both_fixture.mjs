#!/usr/bin/env node
// Regression fixture: a WeChat 4.x-era main package shipping BOTH app-wxss.js
// (stylesheet-only) and page-frame.js (renderer registry). Guards the branch
// priority fix: page-frame.js must be the renderer source and must not be
// deleted when app-wxss.js is present.
import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
const HEADER_LENGTH = 14;
const UINT32_MAX = 0xffff_ffff;
const PAGE_FRAME_JS = [
    "(function(z){var a=11;function Z(ops){z.push(ops)}",
    "Z([3,'index']);",
    "Z([3,'hello-both']);",
    "})(z);__WXML_GLOBAL__.ops_set.$gwx=z;",
    "function np_0(){var nv_module={nv_exports:{}};nv_module.nv_exports=({nv_msg:'hello'});return nv_module.nv_exports;}",
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
    "_(a,b);",
    "_(b,c);",
    "return a;",
    "}};",
    "if(path&&e_[path]){e_[path].call(null)}",
].join("\n");
const FILES = [
    ["app.js", "App({onLaunch(){}});\n"],
    ['app.json', '{"pages":["pages/index/index"],"window":{"navigationBarTitleText":"both"}}\n'],
    ["app.wxss", "page{background:#fff}\n"],
    ["app-config.json", '{"pages":["pages/index/index"]}\n'],
    ["app-service.js", "App({onLaunch(){}});\nPage({data:{}});\n"],
    ["page-frame.js", PAGE_FRAME_JS],
    ["app-wxss.js", "(function(z){var a=11;function Z(ops){z.push(ops)}Z([3,'style']);})(z);__WXML_GLOBAL__.ops_set.$gwx=z;var _C=[];\n"],
    ["pages/index/index.js", "Page({data:{}});\n"],
];
function assertUint32(value, label) {
    if (!Number.isSafeInteger(value) || value < 0 || value > UINT32_MAX) throw new Error(`${label} exceeds uint32`);
}
const entries = FILES.map(([name, content]) => ({ name, nameBytes: Buffer.from(name, "utf8"), content: Buffer.from(content, "utf8") }));
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
const out = resolve(process.argv[2] || "/tmp/both-fixture.wxapkg");
mkdirSync(dirname(out), { recursive: true });
writeFileSync(out, Buffer.concat([header, index, ...entries.map((e) => e.content)]), { mode: 0o600 });
console.log(`generated ${out} (${HEADER_LENGTH + indexLength + bodyLength} bytes)`);
