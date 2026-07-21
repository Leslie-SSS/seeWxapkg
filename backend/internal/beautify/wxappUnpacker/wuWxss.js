const wu = require("./wuLib.js");
const crypto = require("crypto");
const path = require("path");
const fs = require("fs");
const cssbeautify = require('cssbeautify');
const csstree = require('css-tree');
const {parseScript, propertyName, staticEvaluate, walk} = require('./wuStatic.js');
const {relativeContained, resolveContained} = require('./wuPaths.js');
const diagnostics = require('./wuDiagnostics.js');

const hasOwn = (object, key) => Object.prototype.hasOwnProperty.call(object, key);

function aliasIdentifiers(node) {
    if (!node) return [];
    if (node.type === 'Identifier') return [node.name];
    if (node.type === 'LogicalExpression' || node.type === 'ConditionalExpression') {
        const parts = node.type === 'LogicalExpression'
            ? [node.left, node.right]
            : [node.consequent, node.alternate];
        return parts.flatMap(aliasIdentifiers);
    }
    return [];
}

function evaluateStylesheetInitializer(node, name, env) {
    try {
        return staticEvaluate(node, env);
    } catch (error) {
        // 4.x bundles initialize their shared table with `table = table || {}`.
        // An absent self-reference is the only unknown value treated as empty.
        if (
            node &&
            node.type === 'LogicalExpression' &&
            (node.operator === '||' || node.operator === '??') &&
            node.left.type === 'Identifier' &&
            node.left.name === name &&
            !hasOwn(env, name)
        ) {
            return staticEvaluate(node.right, env);
        }
        throw error;
    }
}

function directBindings(statements) {
    const bindings = [];
    for (const statement of statements) {
        if (statement.type === 'VariableDeclaration') {
            for (const declaration of statement.declarations) {
                if (declaration.id.type === 'Identifier' && declaration.init) {
                    bindings.push({name: declaration.id.name, value: declaration.init});
                }
            }
        } else if (
            statement.type === 'ExpressionStatement' &&
            statement.expression.type === 'AssignmentExpression' &&
            statement.expression.operator === '=' &&
            statement.expression.left.type === 'Identifier'
        ) {
            bindings.push({name: statement.expression.left.name, value: statement.expression.right});
        }
    }
    return bindings;
}

function findSetCssBody(program) {
    let body = null;
    for (const statement of program.body) {
        if (statement.type === 'FunctionDeclaration' && statement.id && statement.id.name === 'setCssToHead') {
            body = statement.body.body;
            continue;
        }
        if (statement.type !== 'VariableDeclaration') continue;
        for (const declaration of statement.declarations) {
            if (
                declaration.id.type === 'Identifier' &&
                declaration.id.name === 'setCssToHead' &&
                declaration.init &&
                (declaration.init.type === 'FunctionExpression' || declaration.init.type === 'ArrowFunctionExpression') &&
                declaration.init.body.type === 'BlockStatement'
            ) body = declaration.init.body.body;
        }
    }
    return body;
}

function evaluateKnownCondition(node, env) {
    try {
        return {known: true, value: Boolean(staticEvaluate(node, env))};
    } catch (error) {
        if (node.type === 'UnaryExpression' && node.operator === '!') {
            const argument = evaluateKnownCondition(node.argument, env);
            return argument.known ? {known: true, value: !argument.value} : argument;
        }
        if (node.type === 'CallExpression' && node.arguments.length === 1 && node.callee.type === 'MemberExpression') {
            try {
                if (propertyName(node.callee, env) !== 'hasOwnProperty') return {known: false, value: false};
                const target = staticEvaluate(node.callee.object, env);
                const key = staticEvaluate(node.arguments[0], env);
                if (target && typeof target === 'object' && (typeof key === 'string' || typeof key === 'number')) {
                    return {known: true, value: hasOwn(target, key)};
                }
            } catch (ignored) {
                // Fall through to the conservative unknown result.
            }
        }
        return {known: false, value: false};
    }
}

/**
 * Recover the compiler's `_C` stylesheet table without executing package code.
 * Supports both the legacy array literal and 4.x object-table assignments.
 */
function extractStaticStylesheetData(code) {
    const program = parseScript(code);
    const topLevelBindings = directBindings(program.body);
    const setCssBody = findSetCssBody(program);
    const setCssBindings = setCssBody ? directBindings(setCssBody).filter(binding => binding.name === '_C') : [];

    // Discover aliases from direct program bindings and the canonical top-level
    // setCssToHead function only. Nested/shadowed functions never participate.
    const bindingCandidates = [...topLevelBindings, ...setCssBindings];
    const tracked = new Set(['_C']);
    let changed = true;
    while (changed) {
        changed = false;
        for (const binding of bindingCandidates) {
            if (!tracked.has(binding.name)) continue;
            for (const identifier of aliasIdentifiers(binding.value)) {
                if (!tracked.has(identifier)) {
                    tracked.add(identifier);
                    changed = true;
                }
            }
        }
    }

    const env = Object.create(null);

    function processAssignment(assignment, shadowed) {
        if (assignment.operator !== '=') return;
        if (assignment.left.type === 'Identifier') {
            if (!tracked.has(assignment.left.name) || shadowed.has(assignment.left.name)) return;
            const value = evaluateStylesheetInitializer(assignment.right, assignment.left.name, env);
            if (Array.isArray(value) || (value && typeof value === 'object')) env[assignment.left.name] = value;
            return;
        }
        if (assignment.left.type !== 'MemberExpression' || assignment.left.object.type !== 'Identifier') return;
        const tableName = assignment.left.object.name;
        if (!tracked.has(tableName) || shadowed.has(tableName) || !hasOwn(env, tableName)) return;
        const target = env[tableName];
        if (!target || typeof target !== 'object') return;
        const key = propertyName(assignment.left, env);
        const value = staticEvaluate(assignment.right, env);
        if (Array.isArray(value)) target[key] = value;
    }

    function processExpression(expression, shadowed) {
        if (expression.type === 'AssignmentExpression') processAssignment(expression, shadowed);
        else if (expression.type === 'SequenceExpression') {
            for (const item of expression.expressions) processExpression(item, shadowed);
        }
    }

    function processStatement(statement, shadowed = new Set()) {
        try {
            if (statement.type === 'VariableDeclaration') {
                for (const declaration of statement.declarations) {
                    if (
                        declaration.id.type !== 'Identifier' ||
                        !declaration.init ||
                        !tracked.has(declaration.id.name) ||
                        shadowed.has(declaration.id.name)
                    ) continue;
                    const value = evaluateStylesheetInitializer(declaration.init, declaration.id.name, env);
                    if (Array.isArray(value) || (value && typeof value === 'object')) env[declaration.id.name] = value;
                }
            } else if (statement.type === 'ExpressionStatement') {
                processExpression(statement.expression, shadowed);
            } else if (statement.type === 'IfStatement') {
                const condition = evaluateKnownCondition(statement.test, env);
                if (!condition.known) return;
                const selected = condition.value ? statement.consequent : statement.alternate;
                if (selected) processStatement(selected, shadowed);
            } else if (statement.type === 'BlockStatement') {
                const nestedShadowed = new Set(shadowed);
                for (const child of statement.body) {
                    if (child.type !== 'VariableDeclaration' || child.kind === 'var') continue;
                    for (const declaration of child.declarations) {
                        if (declaration.id.type === 'Identifier' && tracked.has(declaration.id.name)) {
                            nestedShadowed.add(declaration.id.name);
                        }
                    }
                }
                for (const child of statement.body) processStatement(child, nestedShadowed);
            }
            // Functions, loops, try/catch and unknown control flow are ignored.
        } catch (error) {
            // A dynamic statement cannot expand the static interpreter's scope.
        }
    }

    for (const statement of program.body) processStatement(statement);
    // `_C` is initialized when setCssToHead runs, after the top-level table has
    // been populated. Only its direct declarations are interpreted here.
    for (const binding of setCssBindings) {
        try {
            const value = evaluateStylesheetInitializer(binding.value, binding.name, env);
            if (Array.isArray(value) || (value && typeof value === 'object')) env._C = value;
        } catch (error) {
            // Keep searching direct declarations; never run the function body.
        }
    }

    if (!hasOwn(env, '_C') || (!Array.isArray(env._C) && (!env._C || typeof env._C !== 'object'))) {
        throw new Error('No statically recoverable _C stylesheet table was found');
    }
    return env._C;
}

function baseStylesheetName(id) {
    const key = String(id);
    if (/^(?:0|[1-9]\d*)$/.test(key)) return key + '.wxss';
    const digest = crypto.createHash('sha256').update(key).digest('hex').slice(0, 16);
    return 'shared-' + digest + '.wxss';
}

function resolveWxssOutput(root, candidate, source) {
    return resolveContained(root, candidate, 'fallback.wxss.output_unsafe', source);
}

function doWxss(dir, cb, mainDir, nowDir) {
    let saveDir = path.resolve(dir);
    let isSubPkg = mainDir && mainDir.length > 0;
    if (isSubPkg) {
        saveDir = path.resolve(mainDir)
    }

    let runList = Object.create(null), pureData = Object.create(null), result = Object.create(null),
        actualPure = Object.create(null), importCnt = Object.create(null), frameName = "", onlyTest = true,
        blockCss = [];//custom block css file which won't be imported by others.(no extension name)
    const unresolvedImports = new Set();
    const staticAstCache = new Map();

    function isPureReference(value) {
        return (typeof value === 'number' || typeof value === 'string') && hasOwn(pureData, value);
    }

    function reportUnresolvedImport(value, cssFile) {
        const key = `${cssFile}\0${String(value)}`;
        if (unresolvedImports.has(key)) return;
        unresolvedImports.add(key);
        diagnostics.partial(
            'fallback.wxss.unresolved_import',
            'A statically referenced shared WXSS entry was missing and has been omitted.',
            cssFile
        );
    }

    function reportImportCycle(cssFile) {
        diagnostics.partial(
            'fallback.wxss.import_cycle',
            'A cycle in the shared WXSS import table was detected and safely omitted.',
            cssFile
        );
    }

    function cssRebuild(data) {//need to bind this as {cssFile:__name__} before call
			let cssFile;

        function statistic(data, ancestors = new Set()) {
            function addStat(id) {
                if (!isPureReference(id)) {
                    reportUnresolvedImport(id, cssFile);
                    return;
                }
                const key = String(id);
                if (ancestors.has(key)) {
                    reportImportCycle(cssFile);
                    return;
                }
                const firstImport = !hasOwn(importCnt, key);
                importCnt[key] = firstImport ? 1 : importCnt[key] + 1;
                if (!firstImport) return;
                const nestedAncestors = new Set(ancestors);
                nestedAncestors.add(key);
                statistic(pureData[key], nestedAncestors);
				}

            if (isPureReference(data)) return addStat(data);
            if (!Array.isArray(data)) return;
            for (const content of data) {
                if (Array.isArray(content) && content[0] === 2) addStat(content[1]);
            }
        }

        function makeup(data, ancestors = new Set()) {
            let isPure = isPureReference(data);
            if (onlyTest) {
					statistic(data);
                if (!isPure) {
                    if (
                        Array.isArray(data) &&
                        data.length === 1 &&
                        Array.isArray(data[0]) &&
                        data[0][0] === 2 &&
                        isPureReference(data[0][1])
                    ) {
                        data = data[0][1];
                        isPure = true;
                    }
						else return "";
					}
                if (!hasOwn(actualPure, data) && !blockCss.includes(wu.changeExt(wu.toDir(cssFile, frameName), ""))) {
                    console.log("Regard " + cssFile + " as pure import file.");
                    actualPure[data] = cssFile;
					}
					return "";
				}
            let nestedAncestors = ancestors;
            if (isPure) {
                const key = String(data);
                if (ancestors.has(key)) {
                    reportImportCycle(cssFile);
                    return "/*! Cyclic static WXSS import omitted. */";
                }
                nestedAncestors = new Set(ancestors);
                nestedAncestors.add(key);
            }
            let res = [], attach = "";
            if (isPure && actualPure[data] != cssFile) {
                if (hasOwn(actualPure, data)) return '@import "' + wu.changeExt(wu.toDir(actualPure[data], cssFile), ".wxss") + '";\n';
                else {
                    res.push("/*! Import by _C[" + data + "], whose real path we cannot found. */");
                    attach = "/*! Import end */";
					}
				}
            let exactData = isPure ? pureData[data] : data;
            if (!Array.isArray(exactData)) {
                reportUnresolvedImport(data, cssFile);
                return "/*! Unresolved static WXSS import omitted. */";
            }
            for (const content of exactData)
                if (Array.isArray(content)) {
                    switch (content[0]) {
                        case 0://rpx
                            res.push(content[1] + "rpx");
                            break;
                        case 1://add suffix, ignore it for restoring correct!
                            break;
                        case 2://import
                            if (isPureReference(content[1])) res.push(makeup(content[1], nestedAncestors));
                            else {
                                reportUnresolvedImport(content[1], cssFile);
                                res.push("/*! Unresolved static WXSS import omitted. */");
                            }
                            break;
						}
                } else if (typeof content === 'string' || typeof content === 'number') res.push(content);
            return res.join("") + attach;
        }

        return () => {
            cssFile = this.cssFile;
            if (!hasOwn(result, cssFile)) result[cssFile] = "";
            result[cssFile] += makeup(data);
			};
		}

    function runStatic(name, code) {
        const handled = new Set();
        const env = Object.create(null);
        env._C = pureData;
        try {
            let ast = staticAstCache.get(name);
            if (!ast || ast.code !== code) {
                ast = {code, value: parseScript(code)};
                staticAstCache.set(name, ast);
            }
            ast = ast.value;

            const collectCalls = (root, cssFile) => {
                walk(root, node => {
                    if (
                        node.type !== 'CallExpression' ||
                        node.callee.type !== 'Identifier' ||
                        node.callee.name !== 'setCssToHead' ||
                        node.arguments.length === 0 ||
                        handled.has(node.range[0])
                    ) return;
                    try {
                        const data = staticEvaluate(node.arguments[0], env);
                        handled.add(node.range[0]);
                        cssRebuild.call({cssFile}, data)();
                    } catch (error) {
                        console.warn('Skipping dynamic setCssToHead call:', error.message);
                    }
                });
            };

            walk(ast, node => {
                if (node.type === 'VariableDeclarator' && node.id.type === 'Identifier' && node.init) {
                    try {
                        env[node.id.name] = staticEvaluate(node.init, env);
                    } catch (error) {
                        // Runtime values are intentionally ignored.
                    }
                }
                if (node.type !== 'AssignmentExpression' || node.left.type !== 'MemberExpression') return;
                if (node.left.object.type !== 'Identifier' || node.left.object.name !== '__wxAppCode__') return;
                try {
                    const entry = propertyName(node.left, env);
                    if (!entry.endsWith('.wxss')) return;
                    const safeEntry = relativeContained(
                        saveDir,
                        entry,
                        'fallback.wxss.registry_output_unsafe',
                        name
                    );
                    if (safeEntry) collectCalls(node.right, safeEntry);
                } catch (error) {
                    diagnostics.partial('fallback.wxss.registry_entry_invalid', `Skipped invalid WXSS registry entry: ${error.message}`, name);
                }
            });
            collectCalls(ast, name);
        } catch (e) {
            diagnostics.partial('fallback.wxss.static_parse_failed', `WXSS static extraction failed: ${e.message}`, name);
            console.error("WXSS static extraction failed:", e.message);
        }
    }

    function preRun(dir, frameFile, mainCode, files, cb) {
		wu.addIO(cb);
        runList['app.wxss'] = mainCode;
        const sourceRoot = path.resolve(nowDir || dir);

        for (let name of files) {
            if (name != frameFile) {
                wu.get(name, code => {
                    code = code.replace(/display:-webkit-box;display:-webkit-flex;/gm, '');
                    code = code.slice(0, code.indexOf("\n"));
                    if (code.indexOf("setCssToHead(") > -1) {
                        const sourceRelative = path.relative(sourceRoot, path.resolve(name)).split(path.sep).join('/');
                        const outputRelative = relativeContained(
                            saveDir,
                            sourceRelative,
                            'fallback.wxss.scanned_output_unsafe',
                            name
                        );
                        if (outputRelative) runList[outputRelative] = code.slice(code.indexOf("setCssToHead("));
                    }
                });
            }
        }
    }

    function runOnce() {
        for (const name of Object.keys(runList)) runStatic(name, runList[name]);
    }

    function transformCss(style) {
        let ast = csstree.parse(style);
        csstree.walk(ast, function (node) {
            if (node.type == "Comment") {//Change the comment because the limit of css-tree
                node.type = "Raw";
                node.value = "\n/*" + node.value + "*/\n";
			}
            if (node.type == "TypeSelector") {
                if (node.name.startsWith("wx-")) node.name = node.name.slice(3);
                else if (node.name == "body") node.name = "page";
			}
            if (node.children) {
                const removeType = ["webkit", "moz", "ms", "o"];
                let list = {};
                node.children.each((son, item) => {
                    if (son.type == "Declaration") {
                        if (list[son.property]) {
                            let a = item, b = list[son.property], x = son, y = b.data, ans = null;
                            if (x.value.type == 'Raw' && x.value.value.startsWith("progid:DXImageTransform")) {
								node.children.remove(a);
                                ans = b;
                            } else if (y.value.type == 'Raw' && y.value.value.startsWith("progid:DXImageTransform")) {
								node.children.remove(b);
                                ans = a;
                            } else {
                                let xValue = x.value.children && x.value.children.head && x.value.children.head.data.name,
                                    yValue = y.value.children && y.value.children.head && y.value.children.head.data.name;
                                if (xValue && yValue) for (let type of removeType) if (xValue == `-${type}-${yValue}`) {
									node.children.remove(a);
                                    ans = b;
									break;
                                } else if (yValue == `-${type}-${xValue}`) {
									node.children.remove(b);
                                    ans = a;
									break;
                                } else {
                                    let mValue = `-${type}-`;
                                    if (xValue.startsWith(mValue)) xValue = xValue.slice(mValue.length);
                                    if (yValue.startsWith(mValue)) yValue = yValue.slice(mValue.length);
								}
                                if (ans === null) ans = b;
							}
                            list[son.property] = ans;
                        } else list[son.property] = item;
					}
				});
                for (let name in list) if (!name.startsWith('-'))
                    for (let type of removeType) {
                        let fullName = `-${type}-${name}`;
                        if (list[fullName]) {
							node.children.remove(list[fullName]);
							delete list[fullName];
						}
					}
			}
		});
        return cssbeautify(csstree.generate(ast), {indent: '    ', autosemicolon: true});
    }

    wu.scanDirByExt(dir, ".html", files => {
        let frameFile = "";
        if (fs.existsSync(path.resolve(dir, "page-frame.html")))
            frameFile = path.resolve(dir, "page-frame.html");
        else if (fs.existsSync(path.resolve(dir, "app-wxss.js")))
            frameFile = path.resolve(dir, "app-wxss.js");
        else if (fs.existsSync(path.resolve(dir, "page-frame.js")))
            frameFile = path.resolve(dir, "page-frame.js");
		else throw Error("page-frame-like file is not found in the package by auto.");
        wu.get(frameFile, code => {
            code = code.replace(/display:-webkit-box;display:-webkit-flex;/gm, '');
            let scriptCode = code;
            //extract script content from html
            if (frameFile.endsWith(".html")) {
                const scripts = [];
                const scriptPattern = /<script\b[^>]*>([\s\S]*?)<\/script\s*>/gi;
                let match;
                while ((match = scriptPattern.exec(code)) !== null) scripts.push(match[1]);
                if (scripts.length > 0) scriptCode = scripts.join('\n');
            }

            let window = {
                screen: {
                    width: 720,
                    height: 1028,
                    orientation: {
                        type: 'vertical'
                    }
                }
            };
            let navigator = {
                userAgent: "iPhone"
            };

            const compilerStart = scriptCode.lastIndexOf('window.__wcc_version__');
            if (compilerStart >= 0) scriptCode = scriptCode.slice(compilerStart);
            let mainCode = 'window= ' + JSON.stringify(window) +
                ';\nnavigator=' + JSON.stringify(navigator) +
                ';\nvar __mainPageFrameReady__ = window.__mainPageFrameReady__ || function(){};var __WXML_GLOBAL__={entrys:{},defines:{},modules:{},ops:[],wxs_nf_init:undefined,total_ops:0};var __vd_version_info__=__vd_version_info__||{}' +
                ";\n" + scriptCode;

            //remove setCssToHead function
            mainCode = mainCode.replace('var setCssToHead = function', 'var setCssToHead2 = function');

            try {
                pureData = extractStaticStylesheetData(scriptCode);
            } catch (err) {
                diagnostics.partial('fallback.wxss.data_parse_failed', `WXSS _C data could not be parsed statically: ${err.message}`, frameFile);
                console.error("WXSS extraction failed to parse _C statically. Defaulting to empty pureData.");
                pureData = Object.create(null);
            }

			console.log("Guess wxss(first turn)...");
            preRun(dir, frameFile, mainCode, files, () => {
                frameName = relativeContained(
                    saveDir,
                    path.relative(path.resolve(nowDir || dir), frameFile).split(path.sep).join('/'),
                    'fallback.wxss.frame_output_unsafe',
                    frameFile
                ) || 'page-frame.html';
                onlyTest = true;
				runOnce();
                onlyTest = false;
                console.log("Import count info: %j", importCnt);
                for (const id of Object.keys(pureData)) if (!hasOwn(actualPure, id)) {
                    if (!hasOwn(importCnt, id)) importCnt[id] = 0;
                    if (importCnt[id] <= 1) {
                        console.log("Cannot find pure import for _C[" + id + "] which is only imported " + importCnt[id] + " times. Let importing become copying.");
                    } else {
                        let newFile = relativeContained(
                            saveDir,
                            "__wuBaseWxss__/" + baseStylesheetName(id),
                            'fallback.wxss.base_output_unsafe',
                            frameFile
                        );
                        if (!newFile) continue;
                        console.log("Cannot find pure import for _C[" + id + "], force to save it in (" + newFile + ").");
                        actualPure[id] = newFile;
                        cssRebuild.call({cssFile: newFile}, id)();
					}
				}
				console.log("Guess wxss(first turn) done.\nGenerate wxss(second turn)...");
				runOnce()
				console.log("Generate wxss(second turn) done.\nSave wxss...");

                console.log('saveDir: ' + saveDir);
                for (const name of Object.keys(result)) {
                    const output = resolveWxssOutput(saveDir, wu.changeExt(name, ".wxss"), frameFile);
                    if (output) wu.save(output, transformCss(result[name]));
                }
                let delFiles = {};
                for (let name of files) delFiles[name] = 8;
                delFiles[frameFile] = 4;
				cb(delFiles);
			});
		});
	});
}

module.exports = {
    doWxss,
    resolveWxssOutput,
    _internals: {
        baseStylesheetName,
        extractStaticStylesheetData
    }
};
if (require.main === module) {
    wu.commandExecute(doWxss, "Restore wxss files.\n\n<dirs...>\n\n<dirs...> restore wxss file from a unpacked directory(Have page-frame.html (or app-wxss.js) and other html file).");
}
