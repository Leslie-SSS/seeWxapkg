/*
 * Small, deliberately incomplete static-analysis helpers for the legacy
 * unpacker. Nothing in this module evaluates package code.
 */

const esprima = require('esprima');
const path = require('path');

const FORBIDDEN_KEYS = new Set(['__proto__', 'constructor', 'prototype']);

function parseScript(code) {
    return esprima.parseScript(code, {
        comment: false,
        range: true,
        tolerant: true
    });
}

function walk(node, visitor, parent = null) {
    if (!node || typeof node !== 'object') return;
    if (typeof node.type === 'string') visitor(node, parent);
    for (const key of Object.keys(node)) {
        if (key === 'loc' || key === 'range' || key === 'tokens' || key === 'comments') continue;
        const value = node[key];
        if (Array.isArray(value)) {
            for (const child of value) walk(child, visitor, node);
        } else if (value && typeof value === 'object') {
            walk(value, visitor, node);
        }
    }
}

function propertyName(node, env = Object.create(null)) {
    if (!node) throw new Error('Missing property node');
    if (!node.computed && node.property && node.property.type === 'Identifier') {
        return node.property.name;
    }
    const value = staticEvaluate(node.property || node, env);
    if (typeof value !== 'string' && typeof value !== 'number') {
        throw new Error('Non-static property name');
    }
    const name = String(value);
    if (FORBIDDEN_KEYS.has(name)) throw new Error(`Forbidden property name: ${name}`);
    return name;
}

function staticEvaluate(node, env = Object.create(null)) {
    if (!node) return undefined;
    switch (node.type) {
        case 'Literal':
            return node.value;
        case 'Identifier':
            if (node.name === 'undefined') return undefined;
            if (Object.prototype.hasOwnProperty.call(env, node.name)) return env[node.name];
            throw new Error(`Unknown identifier: ${node.name}`);
        case 'ArrayExpression':
            return node.elements.map(element => element ? staticEvaluate(element, env) : undefined);
        case 'ObjectExpression': {
            const result = Object.create(null);
            for (const property of node.properties) {
                if (property.type !== 'Property' || property.kind !== 'init' || property.method) {
                    throw new Error('Unsupported object property');
                }
                const key = property.computed
                    ? String(staticEvaluate(property.key, env))
                    : String(property.key.name ?? property.key.value);
                if (FORBIDDEN_KEYS.has(key)) throw new Error(`Forbidden object key: ${key}`);
                result[key] = staticEvaluate(property.value, env);
            }
            return result;
        }
        case 'UnaryExpression': {
            const value = staticEvaluate(node.argument, env);
            switch (node.operator) {
                case '+': return +value;
                case '-': return -value;
                case '!': return !value;
                case '~': return ~value;
                case 'void': return undefined;
                case 'typeof': return typeof value;
                default: throw new Error(`Unsupported unary operator: ${node.operator}`);
            }
        }
        case 'BinaryExpression': {
            const left = staticEvaluate(node.left, env);
            const right = staticEvaluate(node.right, env);
            switch (node.operator) {
                case '+': return left + right;
                case '-': return left - right;
                case '*': return left * right;
                case '/': return left / right;
                case '%': return left % right;
                case '**': return left ** right;
                case '<<': return left << right;
                case '>>': return left >> right;
                case '>>>': return left >>> right;
                case '|': return left | right;
                case '^': return left ^ right;
                case '&': return left & right;
                case '==': return left == right; // Compiler-generated data can use coercive equality.
                case '!=': return left != right;
                case '===': return left === right;
                case '!==': return left !== right;
                case '<': return left < right;
                case '<=': return left <= right;
                case '>': return left > right;
                case '>=': return left >= right;
                default: throw new Error(`Unsupported binary operator: ${node.operator}`);
            }
        }
        case 'LogicalExpression': {
            const left = staticEvaluate(node.left, env);
            if (node.operator === '&&') return left && staticEvaluate(node.right, env);
            if (node.operator === '||') return left || staticEvaluate(node.right, env);
            if (node.operator === '??') return left ?? staticEvaluate(node.right, env);
            throw new Error(`Unsupported logical operator: ${node.operator}`);
        }
        case 'ConditionalExpression':
            return staticEvaluate(node.test, env)
                ? staticEvaluate(node.consequent, env)
                : staticEvaluate(node.alternate, env);
        case 'TemplateLiteral': {
            let result = '';
            for (let index = 0; index < node.quasis.length; index += 1) {
                result += node.quasis[index].value.cooked;
                if (index < node.expressions.length) result += String(staticEvaluate(node.expressions[index], env));
            }
            return result;
        }
        case 'MemberExpression': {
            const object = staticEvaluate(node.object, env);
            if (object === null || typeof object !== 'object') throw new Error('Invalid static member access');
            const key = propertyName(node, env);
            if (!Object.prototype.hasOwnProperty.call(object, key) && !(Array.isArray(object) && key === 'length')) {
                throw new Error(`Unknown static member: ${key}`);
            }
            return object[key];
        }
        case 'SequenceExpression': {
            let result;
            for (const expression of node.expressions) result = staticEvaluate(expression, env);
            return result;
        }
        default:
            throw new Error(`Unsupported static expression: ${node.type}`);
    }
}

function extractDefineModules(code) {
    const ast = parseScript(code);
    const modules = [];
    walk(ast, node => {
        if (node.type !== 'CallExpression' || node.callee.type !== 'Identifier' || node.callee.name !== 'define') return;
        if (node.arguments.length < 2 || node.arguments[0].type !== 'Literal' || typeof node.arguments[0].value !== 'string') return;
        const factory = [...node.arguments].reverse().find(argument =>
            argument.type === 'FunctionExpression' || argument.type === 'ArrowFunctionExpression');
        if (!factory || !factory.body || factory.body.type !== 'BlockStatement') return;
        modules.push({
            body: code.slice(factory.body.range[0] + 1, factory.body.range[1] - 1),
            name: node.arguments[0].value
        });
    });
    return modules;
}

function safeResolve(root, candidate) {
    if (typeof candidate !== 'string' || candidate.includes('\0')) {
        throw new Error('Invalid output path');
    }
    const normalized = candidate.replace(/\\/g, '/').replace(/^\/+/, '');
    const segments = normalized.split('/');
    if (segments.some(segment => segment === '..')) throw new Error(`Unsafe output path: ${candidate}`);
    const absoluteRoot = path.resolve(root);
    const output = path.resolve(absoluteRoot, normalized);
    if (output !== absoluteRoot && !output.startsWith(absoluteRoot + path.sep)) {
        throw new Error(`Unsafe output path: ${candidate}`);
    }
    return output;
}

module.exports = {
    extractDefineModules,
    parseScript,
    propertyName,
    safeResolve,
    staticEvaluate,
    walk
};
