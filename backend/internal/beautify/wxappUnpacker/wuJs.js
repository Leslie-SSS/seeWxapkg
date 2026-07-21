const wu = require("./wuLib.js");
const path = require("path");
const UglifyJS = require("uglify-es");
const {extractDefineModules, safeResolve} = require('./wuStatic.js');
const diagnostics = require('./wuDiagnostics.js');

function jsBeautify(code) {
    try {
        const result = UglifyJS.minify(code, {
            mangle: false,
            compress: false,
            output: {beautify: true, comments: true}
        });
        if (result.error || typeof result.code !== 'string') throw result.error || new Error('No formatter output');
        return result.code;
    } catch (error) {
        // Never feed untrusted code to an execution-based unpacker. Preserve it
        // byte-for-byte when the static formatter cannot parse the syntax.
        return code;
    }
}

function splitJs(name, cb, mainDir) {
    let isSubPkg = mainDir && mainDir.length > 0;
    let dir = path.dirname(name);
    if (isSubPkg) {
        dir = mainDir;
    }
    wu.get(name, code => {
        let needDelList = {};
        try {
            const modules = extractDefineModules(code);
            for (const module of modules) {
                const output = safeResolve(dir, module.name);
                console.log('Static JS module:', output);
                needDelList[output] = -8;
                wu.save(output, jsBeautify(module.body.trim()));
            }
            if (modules.length > 0) {
                needDelList[path.resolve(name)] = 8;
                console.log(`Statically split ${modules.length} module(s) from "${name}".`);
            } else {
                needDelList[path.resolve(name)] = 0;
                diagnostics.partial('fallback.js.no_static_modules', 'No statically recoverable define() modules were found; the original bundle was preserved.', name);
                console.warn(`No statically recoverable define() modules found in "${name}"; bundle preserved.`);
            }
        } catch (error) {
            needDelList[path.resolve(name)] = 0;
            diagnostics.partial('fallback.js.static_parse_failed', `Static JavaScript split failed; the original bundle was preserved: ${error.message}`, name);
            console.error(`Static JS split failed for "${name}"; bundle preserved:`, error.message);
        }
        cb(needDelList);
    });
}

module.exports = {jsBeautify: jsBeautify, wxsBeautify: jsBeautify, splitJs: splitJs};
if (require.main === module) {
    wu.commandExecute(splitJs, "Split and beautify weapp js file.\n\n<files...>\n\n<files...> js files to split and beautify.");
}
