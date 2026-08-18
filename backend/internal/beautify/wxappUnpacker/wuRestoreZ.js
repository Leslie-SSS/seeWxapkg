const {parseScript, staticEvaluate} = require('./wuStatic.js');
const diagnostics = require('./wuDiagnostics.js');

const UNRESOLVED_OPCODE = Symbol('seewx.unresolvedOpcode');
const UNRESOLVED_VALUE = Symbol('seewx.unresolvedWxmlValue');

function unresolvedOpcode(reason) {
	return Object.freeze({
		[UNRESOLVED_OPCODE]: true,
		reason
	});
}

function containsUnresolvedOpcode(value, seen = new Set()) {
	if (!value || typeof value !== 'object') return false;
	if (value[UNRESOLVED_OPCODE]) return true;
	if (seen.has(value)) return false;
	seen.add(value);
	if (Array.isArray(value)) return value.some(item => containsUnresolvedOpcode(item, seen));
	return Object.values(value).some(item => containsUnresolvedOpcode(item, seen));
}

function unresolvedValue(opcode) {
	return Object.freeze({
		[UNRESOLVED_VALUE]: true,
		reason: opcode.reason
	});
}

function isUnresolvedValue(value) {
	return Boolean(value && typeof value === 'object' && value[UNRESOLVED_VALUE]);
}

function opcodeBuilderBody(ast) {
	const candidates = [];
	for (const statement of ast.body) {
		if (statement.type !== 'ExpressionStatement') continue;
		const call = statement.expression;
		if (call.type !== 'CallExpression' || call.callee.type !== 'FunctionExpression') continue;
		const fn = call.callee;
		if (fn.params.length !== 1 || fn.params[0].type !== 'Identifier' || fn.params[0].name !== 'z') continue;
		const zDeclarations = fn.body.body.filter(item =>
			item.type === 'FunctionDeclaration' && item.id && item.id.name === 'Z');
		if (zDeclarations.length !== 1) continue;
		candidates.push(fn.body.body);
	}
	if (candidates.length !== 1) {
		throw new Error(`Expected one WXML opcode builder, found ${candidates.length}`);
	}
	return candidates[0];
}

function collectStaticZ(code) {
	const values = [];
	const statements = opcodeBuilderBody(parseScript(code));
	const env = Object.create(null);
	env.z = values;

	// Only evaluate the opcode builder's direct, straight-line statements.
	// Calls nested in functions or branches are not known to execute and must
	// never consume a z-table slot. Unsupported executable control flow aborts
	// this table rather than returning confidently misaligned source.
	for (const statement of statements) {
		if (statement.type === 'FunctionDeclaration' || statement.type === 'EmptyStatement') continue;
		if (statement.type === 'ReturnStatement') break;
		if (statement.type === 'VariableDeclaration') {
			for (const declaration of statement.declarations) {
				if (declaration.id.type !== 'Identifier' || !declaration.init) {
					throw new Error('Unsupported WXML opcode builder declaration');
				}
				const name = declaration.id.name;
				if (name === 'z' || name === 'Z') throw new Error(`Unsafe WXML opcode binding: ${name}`);
				env[name] = staticEvaluate(declaration.init, env);
			}
			continue;
		}
		if (statement.type === 'IfStatement') {
			const test = staticEvaluate(statement.test, env);
			if (test === false && !statement.alternate) continue;
			throw new Error('Unsupported WXML opcode builder branch');
		}
		if (statement.type !== 'ExpressionStatement') {
			throw new Error(`Unsupported WXML opcode builder statement: ${statement.type}`);
		}
		const node = statement.expression;
		if (
			node.type !== 'CallExpression' ||
			node.callee.type !== 'Identifier' ||
			node.callee.name !== 'Z' ||
			node.arguments.length === 0
		) {
			throw new Error('Unsupported executable statement in WXML opcode builder');
		}
		try {
			const value = staticEvaluate(node.arguments[0], env);
			if (containsUnresolvedOpcode(value)) {
				values.push(unresolvedOpcode('depends on an unresolved opcode'));
			} else {
				values.push(value);
			}
		} catch (error) {
			// Keep the slot so later z[n] references do not shift onto unrelated
			// opcodes. The renderer will aggregate referenced unknowns per WXML file.
			values.push(unresolvedOpcode(error.message));
			console.warn('Preserving unresolved WXML opcode slot:', error.message);
		}
	}
	if (env.a !== 11) throw new Error('Unexpected WXML value-list opcode');
	return values;
}

function catchZGroup(code, groupPreStr, cb) {
	let zArr = {};
	for (let preStr of groupPreStr) {
		let content = code.slice(code.indexOf(preStr));
		content = content.slice(content.indexOf("(function(z){var a=11;"));
		content = content.slice(0, content.indexOf("})(__WXML_GLOBAL__.ops_cached.$gwx")) + "})(z);";
		let z = [];
		try {
			z = collectStaticZ(content);
		} catch (error) {
			diagnostics.partial('fallback.wxml.group_opcode_parse_failed', `Unable to statically recover grouped WXML opcodes: ${error.message}`);
			console.warn('Unable to statically recover grouped WXML opcodes:', error.message);
		}
		// The renderer selects the group via the callee name suffix
		// (`gz$gwx_1` -> `_1`), so the key must match that suffix exactly.
		// WeChat 4.x emits both `gz$gwx_1` and `gz$gwx12_34` shapes.
		const suffixMatch = preStr.match(/function gz\$gwx([a-zA-Z0-9_]+)/);
		zArr[suffixMatch ? suffixMatch[1] : '0'] = z;
	}
	cb({"mul": zArr});
}

function catchZ(code, cb) {
	// WeChat 4.x groups its z-tables per page/component under
	// `function gz$gwx_1(){...}` (and `gz$gwx12_34` variants); the classic
	// shape required a digit-underscore-digit suffix and never matched.
	let groupTest = code.match(/function gz\$gwx([a-zA-Z0-9_]+)\(\)\{\s*if\( __WXML_GLOBAL__\.ops_cached\.\$gwx/g);
	if (groupTest !== null) return catchZGroup(code, groupTest, cb);
	let z = [];
	let lastPtr = code.lastIndexOf("(z);__WXML_GLOBAL__.ops_set.$gwx=z;");
	if (lastPtr == -1) lastPtr = code.lastIndexOf("(z);__WXML_GLOBAL__.ops_set.$gwx");
	code = code.slice(code.lastIndexOf('(function(z){var a=11;function Z(ops){z.push(ops)}'), lastPtr + 4);
	try {
		z = collectStaticZ(code);
	} catch (error) {
		diagnostics.partial('fallback.wxml.opcode_parse_failed', `Unable to statically recover WXML opcodes: ${error.message}`);
		console.warn('Unable to statically recover WXML opcodes:', error.message);
	}
	cb(z);
}

function restoreSingle(ops, withScope = false) {
	if (typeof ops == "undefined") return "";
	if (ops && typeof ops === 'object' && ops[UNRESOLVED_OPCODE]) return unresolvedValue(ops);

	function scope(value) {
		if (value.startsWith('{') && value.endsWith('}')) return withScope ? value : "{" + value + "}";
		return withScope ? value : "{{" + value + "}}";
	}

	function enBrace(value, type = '{') {
		if (value.startsWith('{') || value.startsWith('[') || value.startsWith('(') || value.endsWith('}') || value.endsWith(']') || value.endsWith(')')) value = ' ' + value + ' ';
		switch (type) {
			case '{':
				return '{' + value + '}';
			case '[':
				return '[' + value + ']';
			case '(':
				return '(' + value + ')';
			default:
				throw Error("Unknown brace type " + type);
		}
	}

	function restoreNext(ops, w = withScope) {
		return restoreSingle(ops, w);
	}

	function jsoToWxon(obj) {//convert JS Object to Wechat Object Notation(No quotes@key+str)
		let ans = "";
		if (typeof obj === "undefined") {
			return 'undefined';
		} else if (obj === null) {
			return 'null';
		} else if (obj instanceof RegExp) {
			return obj.toString();
		} else if (obj instanceof Array) {
			for (let i = 0; i < obj.length; i++) ans += ',' + jsoToWxon(obj[i]);
			return enBrace(ans.slice(1), '[');
		} else if (typeof obj == "object") {
			for (let k in obj) ans += "," + k + ":" + jsoToWxon(obj[k]);
			return enBrace(ans.slice(1), '{');
		} else if (typeof obj == "string") {
			let parts = obj.split('"'), ret = [];
			for (let part of parts) {
				let atoms = part.split("'"), ans = [];
				for (let atom of atoms) ans.push(JSON.stringify(atom).slice(1, -1));
				ret.push(ans.join("\\'"));
			}
			return "'" + ret.join('"') + "'";
		} else return JSON.stringify(obj);
	}

	let op = ops[0];
	if (typeof op != "object") {
		switch (op) {
			case 3://string
				return ops[1];//may cause problems if wx use it to be string
			case 1://direct value
				return scope(jsoToWxon(ops[1]));
			case 11://values list, According to var a = 11;
				let ans = "";
				// Do not mutate the shared opcode array: modern bundles commonly use
				// Z(z[n]), so multiple table entries can reference the same object.
				for (let index = 1; index < ops.length; index++) ans += restoreNext(ops[index]);
				return ans;
		}
	} else {
		let ans = "";
		switch (op[0]) {//vop
			case 2://arithmetic operator
			{
				function getPrior(op, len) {
					const priorList = {
						"?:": 4,
						"&&": 6,
						"||": 5,
						"+": 13,
						"*": 14,
						"/": 14,
						"%": 14,
						"|": 7,
						"^": 8,
						"&": 9,
						"!": 16,
						"~": 16,
						"===": 10,
						"==": 10,
						"!=": 10,
						"!==": 10,
						">=": 11,
						"<=": 11,
						">": 11,
						"<": 11,
						"<<": 12,
						">>": 12,
						"-": len == 3 ? 13 : 16
					};
					return priorList[op] ? priorList[op] : 0;
				}

				function getOp(i) {
					let ret = restoreNext(ops[i], true);
					if (ops[i] instanceof Object && typeof ops[i][0] == "object" && ops[i][0][0] == 2) {
						//Add brackets if we need
						if (getPrior(op[1], ops.length) > getPrior(ops[i][0][1], ops[i].length)) ret = enBrace(ret, '(');
						;
					}
					return ret;
				}

				switch (op[1]) {
					case"?:":
						ans = getOp(1) + "?" + getOp(2) + ":" + getOp(3);
						break;
					case "!":
					case "~":
						ans = op[1] + getOp(1);
						break;
					case"-":
						if (ops.length != 3) {
							ans = op[1] + getOp(1);
							break;
						}//shoud not add more in there![fall through]
					default:
						ans = getOp(1) + op[1] + getOp(2);
				}
				break;
			}
			case 4://unkown-arrayStart?
				ans = restoreNext(ops[1], true);
				break;
			case 5://merge-array
			{
				switch (ops.length) {
					case 2:
						ans = enBrace(restoreNext(ops[1], true), '[');
						break;
					case 1:
						ans = '[]';
						break;
					default: {
						let a = restoreNext(ops[1], true);
						//console.log(a,a.startsWith('[')&&a.endsWith(']'));
						if (a.startsWith('[') && a.endsWith(']')) {
							if (a != '[]') {
								ans = enBrace(a.slice(1, -1).trim() + ',' + restoreNext(ops[2], true), '[');
								//console.log('-',a);
							} else {
								ans = enBrace(restoreNext(ops[2], true), '[');
							}
						} else {
							ans = enBrace('...' + a + ',' + restoreNext(ops[2], true), '[');//may/must not support in fact
						}
					}
				}
				break;
			}
			case 6://get value of an object
			{
				let sonName = restoreNext(ops[2], true);
				if (sonName._type === "var")
					ans = restoreNext(ops[1], true) + enBrace(sonName, '[');
				else {
					let attach = "";
					if (/^[A-Za-z\_][A-Za-z\d\_]*$/.test(sonName)/*is a qualified id*/)
						attach = '.' + sonName;
					else attach = enBrace(sonName, '[');
					ans = restoreNext(ops[1], true) + attach;
				}
				break;
			}
			case 7://get value of str
			{
				switch (ops[1][0]) {
					case 11:
						ans = enBrace("__unTestedGetValue:" + enBrace(jsoToWxon(ops), '['), '{');
						break;
					case 3:
						ans = new String(ops[1][1]);
						ans._type = "var";
						break;
					default:
						throw Error("Unknown type to get value");
				}
				break;
			}
			case 8://first object
				ans = enBrace(ops[1] + ':' + restoreNext(ops[2], true), '{');//ops[1] have only this way to define
				break;
			case 9://object
			{
				function type(x) {
					if (x.startsWith('...')) return 1;
					if (x.startsWith('{') && x.endsWith('}')) return 0;
					return 2;
				}

				let a = restoreNext(ops[1], true);
				let b = restoreNext(ops[2], true);
				let xa = type(a), xb = type(b);
				if (xa == 2 || xb == 2) ans = enBrace("__unkownMerge:" + enBrace(a + "," + b, '['), '{');
				else {
					if (!xa) a = a.slice(1, -1).trim();
					if (!xb) b = b.slice(1, -1).trim();
					//console.log(l,r);
					ans = enBrace(a + ',' + b, '{');
				}
				break;
			}
			case 10://...object
				ans = '...' + restoreNext(ops[1], true);
				break;
			case 12: {
				let arr = restoreNext(ops[2], true);
				if (arr.startsWith('[') && arr.endsWith(']'))
					ans = restoreNext(ops[1], true) + enBrace(arr.slice(1, -1).trim(), '(');
				else ans = restoreNext(ops[1], true) + '.apply' + enBrace('null,' + arr, '(');
				break;
			}
			default:
				ans = enBrace("__unkownSpecific:" + jsoToWxon(ops), '{');
		}
		return scope(ans);
	}
}

function restoreGroup(z) {
	let ans = [];
	for (let g in z.mul) {
		let singleAns = [];
		for (let e of z.mul[g]) singleAns.push(restoreSingle(e, false));
		ans[g] = singleAns;
	}
	let ret = [];//Keep a null array for remaining global Z array.
	ret.mul = ans;
	return ret;
}

function restoreAll(z) {
	if (z.mul) return restoreGroup(z);
	let ans = [];
	for (let e of z) ans.push(restoreSingle(e, false));
	return ans;
}

module.exports = {
	getZ(code, cb) {
		catchZ(code, z => cb(restoreAll(z)));
	},
	collectStaticZ,
	isUnresolvedValue,
	restoreAll
};
