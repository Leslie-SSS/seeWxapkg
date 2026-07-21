const wu = require("./wuLib.js");
const {getZ, isUnresolvedValue} = require("./wuRestoreZ.js");
const {wxsBeautify} = require("./wuJs.js");
const path = require("path");
const esprima = require('esprima');
const escodegen = require('escodegen');
const {parseScript, propertyName, staticEvaluate} = require('./wuStatic.js');
const {packageJoin, relativeContained, resolveContained} = require('./wuPaths.js');
const diagnostics = require('./wuDiagnostics.js');

function resolveWxmlOutput(root, candidate, source) {
    return resolveContained(root, candidate, 'fallback.wxml.output_unsafe', source);
}

function sourceFunction(node, source, env) {
    let returnValue;
    if (node.body.type !== 'BlockStatement') {
        try {
            returnValue = staticEvaluate(node.body, env);
        } catch (error) {
            // A concise arrow may still depend on runtime data.
        }
        return {
            __staticFunction: true,
            returnValue,
            source: source.slice(node.range[0], node.range[1]),
            toString() {
                return this.source;
            }
        };
    }
    const localEnv = Object.assign(Object.create(null), env);
    let returnIsCertain = true;
    for (const statement of node.body.body) {
        if (statement.type === 'FunctionDeclaration' || statement.type === 'EmptyStatement') continue;
        if (statement.type === 'VariableDeclaration') {
            for (const declaration of statement.declarations) {
                if (declaration.id.type !== 'Identifier' || !declaration.init) {
                    returnIsCertain = false;
                    break;
                }
                try {
                    localEnv[declaration.id.name] = staticEvaluate(declaration.init, localEnv);
                } catch (error) {
                    returnIsCertain = false;
                    break;
                }
            }
            if (!returnIsCertain) break;
            continue;
        }
        if (statement.type === 'IfStatement') {
            try {
                if (staticEvaluate(statement.test, localEnv) === false && !statement.alternate) continue;
            } catch (error) {
                // Dynamic control flow means a later direct return is uncertain.
            }
            returnIsCertain = false;
            break;
        }
        if (statement.type === 'ReturnStatement') {
            if (!statement.argument) break;
            try {
                returnValue = staticEvaluate(statement.argument, localEnv);
            } catch (error) {
                // A dynamic return value is expected for renderer functions.
            }
            break;
        }
        // Do not infer across loops, try/catch, nested blocks, calls, or
        // assignments whose effect on the returned value is not proven.
        returnIsCertain = false;
        break;
    }
    if (!returnIsCertain) returnValue = undefined;
    return {
        __staticFunction: true,
        returnValue,
        source: source.slice(node.range[0], node.range[1]),
        toString() {
            return this.source;
        }
    };
}

function evaluateRegistryNode(node, source, env) {
    if (!node) return undefined;
    if (node.type === 'FunctionExpression' || node.type === 'ArrowFunctionExpression') {
        return sourceFunction(node, source, env);
    }
    if (node.type === 'ObjectExpression') {
        const result = Object.create(null);
        for (const property of node.properties) {
            if (property.type !== 'Property' || property.kind !== 'init') continue;
            const key = property.computed
                ? String(staticEvaluate(property.key, env))
                : String(property.key.name ?? property.key.value);
            if (key === '__proto__' || key === 'constructor' || key === 'prototype') continue;
            result[key] = evaluateRegistryNode(property.value, source, env);
        }
        return result;
    }
    if (node.type === 'ArrayExpression') {
        return node.elements.map(element => evaluateRegistryNode(element, source, env));
    }
    if (
        node.type === 'CallExpression' &&
        node.callee.type === 'Identifier' &&
        node.callee.name === 'nv_require' &&
        node.arguments.length > 0
    ) {
        return {
            __staticFunction: true,
            returnValue: staticEvaluate(node.arguments[0], env),
            source: '',
            toString() { return this.source; }
        };
    }
    return staticEvaluate(node, env);
}

function extractWxmlRegistries(code) {
    const ast = parseScript(code);
    const env = Object.create(null);
    const registries = {
        d_: Object.create(null),
        e_: Object.create(null),
        f_: Object.create(null)
    };

    const registryNames = new Set(Object.keys(registries));

    // Registry setup emitted by WeChat is a top-level straight-line sequence.
    // Walking the whole AST would also collect assignments in uncalled helper
    // functions and dead branches, corrupting the recovered file registry.
    for (const statement of ast.body) {
        if (statement.type === 'FunctionDeclaration') {
            if (statement.id && registryNames.has(statement.id.name)) {
                throw new Error(`WXML registry is shadowed: ${statement.id.name}`);
            }
            continue;
        }
        if (statement.type === 'EmptyStatement') continue;
        if (statement.type === 'IfStatement') {
            try {
                if (staticEvaluate(statement.test, env) === false && !statement.alternate) continue;
            } catch (error) {
                // Fall through to the conservative failure below.
            }
            throw new Error('Unsupported WXML registry control flow');
        }
        if (statement.type === 'VariableDeclaration') {
            for (const declaration of statement.declarations) {
                if (declaration.id.type !== 'Identifier' || !declaration.init) continue;
                if (registryNames.has(declaration.id.name)) {
                    throw new Error(`WXML registry is shadowed: ${declaration.id.name}`);
                }
                try {
                    env[declaration.id.name] = evaluateRegistryNode(declaration.init, code, env);
                } catch (error) {
                    // Dynamic runtime variables are intentionally ignored.
                }
            }
            continue;
        }
        if (statement.type !== 'ExpressionStatement' || statement.expression.type !== 'AssignmentExpression') {
            throw new Error(`Unsupported WXML registry statement: ${statement.type}`);
        }
        const node = statement.expression;
        if (node.operator !== '=') throw new Error(`Unsupported WXML registry assignment: ${node.operator}`);
        if (node.left.type === 'Identifier') {
            if (registryNames.has(node.left.name)) throw new Error(`WXML registry is reassigned: ${node.left.name}`);
            try {
                env[node.left.name] = evaluateRegistryNode(node.right, code, env);
            } catch (error) {
                // Dynamic assignment.
            }
            continue;
        }
        if (node.left.type !== 'MemberExpression' || node.left.object.type !== 'Identifier') continue;
        const registry = registries[node.left.object.name];
        if (!registry) continue;
        try {
            registry[propertyName(node.left, env)] = evaluateRegistryNode(node.right, code, env);
        } catch (error) {
            console.warn('Skipping dynamic WXML registry entry:', error.message);
        }
    }

    return {env, registries};
}

function analyze(core, z, namePool, xPool, fakePool = {}, zMulName = "0") {
    function anaRecursion(core, fakePool = {}) {
        return analyze(core, z, namePool, xPool, fakePool, zMulName);
    }

    function push(name, elem) {
        namePool[name] = elem;
    }

    function pushSon(pname, son) {
        if (fakePool[pname]) fakePool[pname].son.push(son);
        else namePool[pname].son.push(son);
    }

    for (let ei = 0; ei < core.length; ei++) {
        let e = core[ei];
        switch (e.type) {
            case "ExpressionStatement": {
                let f = e.expression;
                if (f.callee) {
                    if (f.callee.type == "Identifier") {
                        switch (f.callee.name) {
                            case "_r":
                                namePool[f.arguments[0].name].v[f.arguments[1].value] = z[f.arguments[2].value];
                                break;
                            case "_rz":
                                namePool[f.arguments[1].name].v[f.arguments[2].value] = z.mul[zMulName][f.arguments[3].value];
                                break;
                            case "_":
                                pushSon(f.arguments[0].name, namePool[f.arguments[1].name]);
                                break;
                            case "_2": {
                                let item = f.arguments[6].value;//def:item
                                let index = f.arguments[7].value;//def:index
                                let data = z[f.arguments[0].value];
                                let key = escodegen.generate(f.arguments[8]).slice(1, -1);//f.arguments[8].value;//def:""
                                let obj = namePool[f.arguments[5].name];
                                let gen = namePool[f.arguments[1].name];
                                if (gen.tag == "gen") {
                                    let ret = gen.func.body.body.pop().argument.name;
                                    anaRecursion(gen.func.body.body, {[ret]: obj});
                                }
                                obj.v["wx:for"] = data;
                                if (index != "index") obj.v["wx:for-index"] = index;
                                if (item != "item") obj.v["wx:for-item"] = item;
                                if (key != "") obj.v["wx:key"] = key;
                            }
                                break;
                            case "_2z": {
                                let item = f.arguments[7].value;//def:item
                                let index = f.arguments[8].value;//def:index
                                let data = z.mul[zMulName][f.arguments[1].value];
                                let key = escodegen.generate(f.arguments[9]).slice(1, -1);//f.arguments[9].value;//def:""
                                let obj = namePool[f.arguments[6].name];
                                let gen = namePool[f.arguments[2].name];
                                if (gen.tag == "gen") {
                                    let ret = gen.func.body.body.pop().argument.name;
                                    anaRecursion(gen.func.body.body, {[ret]: obj});
                                }
                                obj.v["wx:for"] = data;
                                if (index != "index") obj.v["wx:for-index"] = index;
                                if (item != "item") obj.v["wx:for-item"] = item;
                                if (key != "") obj.v["wx:key"] = key;
                            }
                                break;
                            case "_ic":
                                pushSon(f.arguments[5].name, {
                                    tag: "include",
                                    son: [],
                                    v: {src: xPool[f.arguments[0].property.value]}
                                });
                                break;
                            case "_ai": {//template import
                                let to = Object.keys(fakePool)[0];
                                if (to) pushSon(to, {
                                    tag: "import",
                                    son: [],
                                    v: {src: xPool[f.arguments[1].property.value]}
                                });
                                else throw Error("Unexpected fake pool");
                            }
                                break;
                            case "_af":
                                //ignore _af
                                break;
                            default:
                                throw Error("Unknown expression callee name " + f.callee.name);
                        }
                    } else if (f.callee.type == "MemberExpression") {
                        if (f.callee.object.name == "cs" || f.callee.property.name == "pop") break;
                        throw Error("Unknown member expression");
                    } else throw Error("Unknown callee type " + f.callee.type);
                } else if (f.type == "AssignmentExpression" && f.operator == "=") {
                    //no special use
                } else throw Error("Unknown expression statement.");
                break;
            }
            case "VariableDeclaration":
                for (let dec of e.declarations) {
                    if (dec.init.type == "CallExpression") {
                        switch (dec.init.callee.name) {
                            case "_n":
                                push(dec.id.name, {tag: dec.init.arguments[0].value, son: [], v: {}});
                                break;
                            case "_v":
                                push(dec.id.name, {tag: "block", son: [], v: {}});
                                break;
                            case "_o":
                                push(dec.id.name, {
                                    tag: "__textNode__",
                                    textNode: true,
                                    content: z[dec.init.arguments[0].value]
                                });
                                break;
                            case "_oz":
                                push(dec.id.name, {
                                    tag: "__textNode__",
                                    textNode: true,
                                    content: z.mul[zMulName][dec.init.arguments[1].value]
                                });
                                break;
                            case "_m": {
                                if (dec.init.arguments[2].elements.length > 0)
                                    throw Error("Noticable generics content: " + dec.init.arguments[2].toString());
                                let mv = {};
                                let name = null, base = 0;
                                for (let x of dec.init.arguments[1].elements) {
                                    let v = x.value;
                                    if (!v && typeof v != "number") {
                                        if (x.type == "UnaryExpression" && x.operator == "-") v = -x.argument.value;
                                        else throw Error("Unknown type of object in _m attrs array: " + x.type);
                                    }
                                    if (name === null) {
                                        name = v;
                                    } else {
                                        if (base + v < 0) mv[name] = null; else {
                                            mv[name] = z[base + v];
                                            if (base == 0) base = v;
                                        }
                                        name = null;
                                    }
                                }
                                push(dec.id.name, {tag: dec.init.arguments[0].value, son: [], v: mv});
                            }
                                break;
                            case "_mz": {
                                if (dec.init.arguments[3].elements.length > 0)
                                    throw Error("Noticable generics content: " + dec.init.arguments[3].toString());
                                let mv = {};
                                let name = null, base = 0;
                                for (let x of dec.init.arguments[2].elements) {
                                    let v = x.value;
                                    if (!v && typeof v != "number") {
                                        if (x.type == "UnaryExpression" && x.operator == "-") v = -x.argument.value;
                                        else throw Error("Unknown type of object in _mz attrs array: " + x.type);
                                    }
                                    if (name === null) {
                                        name = v;
                                    } else {
                                        if (base + v < 0) mv[name] = null; else {
                                            mv[name] = z.mul[zMulName][base + v];
                                            if (base == 0) base = v;
                                        }
                                        name = null;
                                    }
                                }
                                push(dec.id.name, {tag: dec.init.arguments[1].value, son: [], v: mv});
                            }
                                break;
                            case "_gd"://template use/is
                            {
                                let is = namePool[dec.init.arguments[1].name].content;
                                let data = null, obj = null;
                                ei++;
                                for (let e of core[ei].consequent.body) {
                                    if (e.type == "VariableDeclaration") {
                                        for (let f of e.declarations) {
                                            if (f.init.type == "LogicalExpression" && f.init.left.type == "CallExpression") {
                                                if (f.init.left.callee.name == "_1") data = z[f.init.left.arguments[0].value];
                                                else if (f.init.left.callee.name == "_1z") data = z.mul[zMulName][f.init.left.arguments[1].value];
                                            }
                                        }
                                    } else if (e.type == "ExpressionStatement") {
                                        let f = e.expression;
                                        if (f.type == "AssignmentExpression" && f.operator == "=" && f.left.property && f.left.property.name == "wxXCkey") {
                                            obj = f.left.object.name;
                                        }
                                    }
                                }
                                namePool[obj].tag = "template";
                                Object.assign(namePool[obj].v, {is: is, data: data});
                            }
                                break;
                            default: {
                                let funName = dec.init.callee.name;
                                if (funName.startsWith("gz$gwx")) {
                                    zMulName = funName.slice(6);
                                } else throw Error("Unknown init callee " + funName);
                            }
                        }
                    } else if (dec.init.type == "FunctionExpression") {
                        push(dec.id.name, {tag: "gen", func: dec.init});
                    } else if (dec.init.type == "MemberExpression") {
                        if (dec.init.object.type == "MemberExpression" && dec.init.object.object.name == "e_" && dec.init.object.property.type == "MemberExpression" && dec.init.object.property.object.name == "x") {
                            if (dec.init.property.name == "j") {//include
                                //do nothing
                            } else if (dec.init.property.name == "i") {//import
                                //do nothing
                            } else throw Error("Unknown member expression declaration.");
                        } else throw Error("Unknown member expression declaration.");
                    } else throw Error("Unknown declaration init type " + dec.init.type);
                }
                break;
            case "IfStatement":
                if (e.test.callee.name.startsWith("_o")) {
                    function parse_OFun(e) {
                        if (e.test.callee.name == "_o") return z[e.test.arguments[0].value];
                        else if (e.test.callee.name == "_oz") return z.mul[zMulName][e.test.arguments[1].value];
                        else throw Error("Unknown if statement test callee name:" + e.test.callee.name);
                    }

                    let vname = e.consequent.body[0].expression.left.object.name;
                    let nif = {tag: "block", v: {"wx:if": parse_OFun(e)}, son: []};
                    anaRecursion(e.consequent.body, {[vname]: nif});
                    pushSon(vname, nif);
                    if (e.alternate) {
                        while (e.alternate && e.alternate.type == "IfStatement") {
                            e = e.alternate;
                            nif = {tag: "block", v: {"wx:elif": parse_OFun(e)}, son: []};
                            anaRecursion(e.consequent.body, {[vname]: nif});
                            pushSon(vname, nif);
                        }
                        if (e.alternate && e.alternate.type == "BlockStatement") {
                            e = e.alternate;
                            nif = {tag: "block", v: {"wx:else": null}, son: []};
                            anaRecursion(e.body, {[vname]: nif});
                            pushSon(vname, nif);
                        }
                    }
                } else throw Error("Unknown if statement.");
                break;
            default:
                throw Error("Unknown type " + e.type);
        }
    }
}

const UNRESOLVED_TEXT_COMMENT = '<!-- seewx-recovery: unresolved text omitted -->';
const UNRESOLVED_ATTRIBUTES_COMMENT = '<!-- seewx-recovery: unresolved attributes omitted -->';

function createWxmlRecoveryContext() {
    return {
        unresolvedTextCount: 0,
        unresolvedAttributeCount: 0,
        unresolvedAttributes: Object.create(null),
        unresolvedEventAttributeCount: 0
    };
}

function isUnavailableValue(value) {
    return typeof value === 'undefined' || value === null || isUnresolvedValue(value);
}

function isEventAttribute(name) {
    return /^(?:(?:capture-)?(?:bind|catch)|mut-bind)(?::?[A-Za-z][\w-]*)$/i.test(name);
}

function recordUnresolvedText(context) {
    context.unresolvedTextCount += 1;
}

function recordUnresolvedAttribute(context, name) {
    context.unresolvedAttributeCount += 1;
    context.unresolvedAttributes[name] = (context.unresolvedAttributes[name] || 0) + 1;
    if (isEventAttribute(name)) context.unresolvedEventAttributeCount += 1;
}

function flushWxmlRecoveryDiagnostics(context, outputPath) {
    const unresolvedCount = context.unresolvedTextCount + context.unresolvedAttributeCount;
    if (unresolvedCount > 0) {
        diagnostics.partial(
            'fallback.wxml.unresolved_fragments',
            `该文件有 ${unresolvedCount} 处运行时值无法安全静态还原（文本 ${context.unresolvedTextCount}，属性 ${context.unresolvedAttributeCount}）；已省略可见占位并用不渲染的注释标记，原始运行时代码仍保留。`,
            outputPath,
            {
                count: unresolvedCount,
                textCount: context.unresolvedTextCount,
                attributeCount: context.unresolvedAttributeCount,
                eventAttributeCount: context.unresolvedEventAttributeCount,
                attributes: {...context.unresolvedAttributes},
                marker: 'seewx-recovery',
                runtimeSourcePreserved: true
            }
        );
    }
}

function wxmlify(str, isText) {
    if (isText) return String(str);//may have some bugs in some specific case(undocumented by tx)
    else return String(str).replace(/"/g, '\\"');
}

function elemToString(elem, dep, moreInfo = false, recoveryContext = createWxmlRecoveryContext()) {
    const longerList = [];//put tag name which can't be <x /> style.
    const indent = ' '.repeat(4);

    function isTextTag(elem) {
        return elem.tag == "__textNode__" && elem.textNode;
    }

    function elemRecursion(elem, dep) {
        return elemToString(elem, dep, moreInfo, recoveryContext);
    }

    function trimMerge(rets) {
        let needTrimLeft = false, ans = "";
        for (let ret of rets) {
            if (ret.textNode == 1) {
                if (!needTrimLeft) {
                    needTrimLeft = true;
                    ans = ans.trimRight();
                }
            } else if (needTrimLeft) {
                needTrimLeft = false;
                ret = ret.trimLeft();
            }
            ans += ret;
        }
        return ans;
    }

    if (isTextTag(elem)) {
        if (isUnavailableValue(elem.content)) {
            recordUnresolvedText(recoveryContext);
            // A recovery comment is not a text node. Treating it as one would
            // activate trimMerge's whitespace folding and remove known spaces
            // immediately before or after an unresolved fragment.
            return UNRESOLVED_TEXT_COMMENT;
        }
        //In comment, you can use typify text node, which beautify its code, but may destroy ui.
        //So, we use a "hack" way to solve this problem by letting typify program stop when face textNode
        let str = new String(wxmlify(elem.content, true));
        str.textNode = 1;
        return wxmlify(str, true);//indent.repeat(dep)+wxmlify(elem.content.trim(),true)+"\n";
    }
    if (elem.tag == "block" && !moreInfo) {
        if (elem.son.length == 1 && !isTextTag(elem.son[0])) {
            let ok = true, s = elem.son[0];
            for (let x in elem.v) if (x in s.v) {
                ok = false;
                break;
            }
            if (ok && !(("wx:for" in s.v || "wx:if" in s.v) && ("wx:if" in elem.v || "wx:else" in elem.v || "wx:elif" in elem.v))) {//if for and if in one tag, the default result is an if in for. And we should block if nested in elif/else been combined.
                Object.assign(s.v, elem.v);
                return elemRecursion(s, dep);
            }
        } else if (Object.keys(elem.v).length == 0) {
            let ret = [];
            for (let s of elem.son) ret.push(elemRecursion(s, dep));
            return trimMerge(ret);
        }
    }
    const unresolvedAttributes = [];
    let ret = indent.repeat(dep) + "<" + elem.tag;
    for (let v in elem.v) {
        const value = elem.v[v];
        // A null attribute is the compiler's representation for a genuine
        // boolean WXML attribute. Undefined values and explicit unresolved
        // opcode markers, however, must not be presented as recovered source.
        if (typeof value === 'undefined' || isUnresolvedValue(value)) {
            unresolvedAttributes.push(v);
            recordUnresolvedAttribute(recoveryContext, v);
            continue;
        }
        // Dynamic event handlers such as bindtap="{{ handlerName }}" are valid
        // WXML and are preserved without lowering recovery quality.
        ret += " " + v + (value !== null ? "=\"" + wxmlify(value) + "\"" : "");
    }
    if (unresolvedAttributes.length > 0) {
        ret = indent.repeat(dep) + UNRESOLVED_ATTRIBUTES_COMMENT + "\n" + ret;
    }
    if (elem.son.length == 0) {
        if (longerList.includes(elem.tag)) return ret + " />\n";
        else return ret + "></" + elem.tag + ">\n";
    }
    ret += ">\n";
    let rets = [ret];
    for (let s of elem.son) rets.push(elemRecursion(s, dep + 1));
    rets.push(indent.repeat(dep) + "</" + elem.tag + ">\n");
    return trimMerge(rets);
}

function doWxml(state, outputPath, code, z, xPool, rDs, wxsList, moreInfo, diagnosticPath = outputPath) {
    const recoveryContext = createWxmlRecoveryContext();
    let rname = code.slice(code.lastIndexOf("return") + 6).replace(/[\;\}]/g, "").trim();
    code = code.slice(code.indexOf("\n"), code.lastIndexOf("return")).trim();
    let r = {son: []};
    analyze(esprima.parseScript(code).body, z, {[rname]: r}, xPool, {[rname]: r});
    let ans = [];
    for (let elem of r.son) ans.push(elemToString(elem, 0, moreInfo, recoveryContext));
    let result = [ans.join("")];
    for (let v in rDs) {
        state[0] = v;
        let oriCode = rDs[v].toString();
        let rname = oriCode.slice(oriCode.lastIndexOf("return") + 6).replace(/[\;\}]/g, "").trim();
        let tryPtr = oriCode.indexOf("\ntry{");
        let zPtr = oriCode.indexOf("var z=gz$gwx");
        let code = oriCode.slice(tryPtr + 5, oriCode.lastIndexOf("\n}catch(")).trim();
        if (zPtr != -1 && tryPtr > zPtr) {
            let attach = oriCode.slice(zPtr);
            attach = attach.slice(0, attach.indexOf("()")) + "()\n";
            code = attach + code;
        }
        let r = {tag: "template", v: {name: v}, son: []};
        analyze(esprima.parseScript(code).body, z, {[rname]: r}, xPool, {[rname]: r});
        result.unshift(elemToString(r, 0, moreInfo, recoveryContext));
    }
    if (wxsList[outputPath]) result.push(wxsList[outputPath]);
    wu.save(outputPath, result.join(""));
    flushWxmlRecoveryDiagnostics(recoveryContext, diagnosticPath);
}

function tryWxml(root, outputPath, code, z, xPool, rDs, ...args) {
    console.log("Decompile " + outputPath + "...");
    let state = [null];
    const relativeOutput = path.relative(path.resolve(root), outputPath).split(path.sep).join('/');
    try {
        doWxml(state, outputPath, code, z, xPool, rDs, ...args, relativeOutput);
        console.log("Decompile success!");
    } catch (e) {
        console.log("error on " + outputPath + "(" + (state[0] === null ? "Main" : "Template-" + state[0]) + ")\nerr: ", e);
        diagnostics.partial('fallback.wxml.render_failed', `WXML render failed; static renderer source was preserved: ${e.message}`, relativeOutput);
        const debugOutput = resolveContained(
            root,
            relativeOutput + (state[0] === null ? '.ori.js' : '.template.ori.js'),
            'fallback.wxml.debug_output_unsafe',
            outputPath
        );
        if (!debugOutput) return;
        const debugContent = state[0] === null
            ? code
            : (rDs[state[0]] && typeof rDs[state[0]].toString === 'function' ? rDs[state[0]].toString() : code);
        wu.save(debugOutput, debugContent);
    }
}

function doWxs(code, name) {
    name = name || '';
    name = name.substring(0, name.lastIndexOf('/') + 1);
    const before = 'nv_module={nv_exports:{}};';
    const prefix = 'p_' + name;
    const escapedPrefix = prefix.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    return wxsBeautify(code.slice(code.indexOf(before) + before.length, code.lastIndexOf('return nv_module.nv_exports;}')).replace(new RegExp(escapedPrefix, 'g'), '').replace(/nv\_/g, '').replace(/(require\(.*?\))\(\)/g,'$1'));
}

function doFrame(name, cb, order, mainDir) {
    let moreInfo = order.includes("m");
    let wxsList = {};
    wu.get(name, code => {
        getZ(code, z => {
            const before = "\nvar nv_require=function(){var nnm=";
            code = code.slice(code.lastIndexOf(before) + before.length, code.lastIndexOf("if(path&&e_[path]){"));
            const json = code.slice(0, code.indexOf("};") + 1);
            let endOfRequire = code.indexOf("()\r\n") + 4;
            if (endOfRequire == 4 - 1) endOfRequire = code.indexOf("()\n") + 3;
            code = code.slice(endOfRequire);
            let rD = {}, rE = {}, rF = {}, requireInfo = {}, x = {};
            try {
                const extracted = extractWxmlRegistries(code);
                rD = extracted.registries.d_;
                rE = extracted.registries.e_;
                rF = extracted.registries.f_;
                if (extracted.env.x && typeof extracted.env.x === 'object') x = extracted.env.x;

                if (json) {
                    const wrapped = parseScript('(' + json + ')');
                    requireInfo = evaluateRegistryNode(wrapped.body[0].expression, '(' + json + ')', extracted.env) || {};
                }
            } catch (err) {
                diagnostics.partial('fallback.wxml.registry_parse_failed', `WXML static registry extraction failed: ${err.message}`, name);
                console.error("WXML static extraction failed. WXML files might be missing. Error:", err.message);
            }
            let dir = mainDir || path.dirname(name), pF = [];
            for (let info in rF) if (rF[info] && rF[info].__staticFunction) {
                const safeInfo = relativeContained(dir, info, 'fallback.wxml.wxs_output_unsafe', name);
                if (!safeInfo) continue;
                const output = resolveContained(dir, safeInfo, 'fallback.wxml.wxs_output_unsafe', name);
                if (!output) continue;
                let ref = rF[info].returnValue;
                if (typeof ref !== 'string') continue;
                pF[ref] = safeInfo;
                if (requireInfo[ref] && typeof requireInfo[ref].toString === 'function') {
                    wu.save(output, doWxs(requireInfo[ref].toString(), safeInfo));
                }
            }
            for (let info in rF) if (rF[info] && typeof rF[info] == "object" && !rF[info].__staticFunction) {
                const safeInfo = relativeContained(dir, info, 'fallback.wxml.wxs_group_output_unsafe', name);
                if (!safeInfo) continue;
                const output = resolveContained(dir, safeInfo, 'fallback.wxml.wxs_group_output_unsafe', name);
                if (!output) continue;
                let res = [], now = rF[info];
                for (let deps in now) {
                    let ref = now[deps] && now[deps].__staticFunction ? now[deps].returnValue : undefined;
                    if (typeof ref !== 'string') continue;
                    const safeModuleName = String(deps).replace(/[&"<>]/g, character => ({'&': '&amp;', '"': '&quot;', '<': '&lt;', '>': '&gt;'}[character]));
                    if (ref.includes(":") && requireInfo[ref] && typeof requireInfo[ref].toString === 'function') {
                        res.push("<wxs module=\"" + safeModuleName + "\">\n" + doWxs(requireInfo[ref].toString()) + "\n</wxs>");
                    } else if (pF[ref]) {
                        res.push("<wxs module=\"" + safeModuleName + "\" src=\"" + wu.toDir(pF[ref], safeInfo) + "\" />");
                    } else {
                        const reference = relativeContained(
                            dir,
                            packageJoin(safeInfo, ref.startsWith('./') ? ref.slice(2) : ref),
                            'fallback.wxml.wxs_reference_unsafe',
                            name
                        );
                        if (!reference) continue;
                        res.push("<wxs module=\"" + safeModuleName + "\" src=\"" + wu.toDir(reference, safeInfo) + "\" />");
                    }
                    wxsList[output] = res.join("\n");
                }
            }
            for (let entryName in rE) {
                const renderer = rE[entryName] && rE[entryName].f;
                if (!renderer || typeof renderer.toString !== 'function') continue;
                const output = resolveWxmlOutput(dir, entryName, name);
                if (!output) continue;
                tryWxml(dir, output, renderer.toString(), z, x, rD[entryName] || {}, wxsList, moreInfo);
            }
            if (Object.keys(rE).length === 0) {
                diagnostics.partial('fallback.wxml.no_static_entries', 'No statically recoverable WXML renderer entries were found; runtime bundle was preserved.', name);
            }
            cb({[name]: 4});
        });
    });
}

module.exports = {
    doFrame: doFrame,
    resolveWxmlOutput,
    _internals: {
        createWxmlRecoveryContext,
        elemToString,
        extractWxmlRegistries,
        flushWxmlRecoveryDiagnostics,
        isEventAttribute,
        sourceFunction
    }
};
if (require.main === module) {
    wu.commandExecute(doFrame, "Restore wxml files.\n\n<files...>\n\n<files...> restore wxml file from page-frame.html or app-wxss.js.");
}
