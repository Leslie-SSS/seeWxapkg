const path = require('path');

const diagnostics = require('./wuDiagnostics.js');
const {safeResolve} = require('./wuStatic.js');

function reportUnsafe(code, candidate, source, error) {
    const rendered = typeof candidate === 'string' ? candidate : String(candidate);
    diagnostics.partial(
        code,
        `Skipped unsafe package-derived path ${JSON.stringify(rendered)}: ${error.message}`,
        source
    );
}

/**
 * Resolve a package-derived path under an output root. Absolute-looking package
 * paths (`/pages/index`) are interpreted as package-root relative. Invalid,
 * empty, or escaping paths are diagnosed and return null.
 */
function resolveContained(root, candidate, code, source) {
    try {
        if (typeof candidate !== 'string' || candidate.length === 0) {
            throw new Error('path must be a non-empty string');
        }
        const absoluteRoot = path.resolve(root);
        const output = safeResolve(absoluteRoot, candidate);
        if (output === absoluteRoot) throw new Error('path resolves to the output root');
        return output;
    } catch (error) {
        reportUnsafe(code, candidate, source, error);
        return null;
    }
}

function relativeContained(root, candidate, code, source) {
    const output = resolveContained(root, candidate, code, source);
    if (!output) return null;
    return path.relative(path.resolve(root), output).split(path.sep).join('/');
}

function packageJoin(base, child) {
    const normalizedBase = typeof base === 'string' ? base.replace(/\\/g, '/') : '';
    const normalizedChild = typeof child === 'string' ? child.replace(/\\/g, '/') : '';
    if (normalizedChild.startsWith('/')) return normalizedChild.replace(/^\/+/, '');
    return path.posix.join(path.posix.dirname(normalizedBase), normalizedChild);
}

module.exports = {
    packageJoin,
    relativeContained,
    resolveContained
};
