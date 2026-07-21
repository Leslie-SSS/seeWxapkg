const wu = require('./wuLib.js');
const fs = require('fs');
const path = require('path');
const crypto = require('crypto');
const {extractDefineModules, parseScript, propertyName, staticEvaluate, walk} = require('./wuStatic.js');
const {packageJoin, relativeContained, resolveContained} = require('./wuPaths.js');
const diagnostics = require('./wuDiagnostics.js');

function changeExtension(candidate, extension = '') {
    if (typeof candidate !== 'string') return '';
    const normalized = candidate.replace(/\\/g, '/');
    const currentExtension = path.posix.extname(normalized);
    return currentExtension
        ? normalized.slice(0, -currentExtension.length) + extension
        : normalized + extension;
}

function getWorkerPath(name) {
    const code = fs.readFileSync(name, {encoding: 'utf8'});
    const root = path.dirname(name);
    let commonPath = false;
    try {
        for (const module of extractDefineModules(code)) {
            const safeModule = relativeContained(
                root,
                module.name,
                'fallback.worker_path.unsafe_module',
                name
            );
            if (!safeModule) continue;
            const moduleDir = path.posix.dirname(safeModule) + '/';
            if (commonPath === false) commonPath = moduleDir;
            commonPath = wu.commonDir(commonPath, moduleDir);
        }
    } catch (error) {
        diagnostics.partial('fallback.worker_path.static_parse_failed', `Unable to determine worker path statically: ${error.message}`, name);
        console.warn('Unable to determine worker path statically:', error.message);
    }
    if (commonPath === false) commonPath = '';
    if (commonPath.length > 0) commonPath = commonPath.slice(0, -1);
    console.log(`Worker path: "${commonPath}"`);
    return commonPath;
}

function sanitizeMainPages(rawPages, dir, source) {
    const pages = [];
    if (!Array.isArray(rawPages)) {
        diagnostics.partial('fallback.config.pages_invalid', 'app-config pages is not an array; no main pages were generated.', source);
        return pages;
    }
    for (const rawPage of rawPages) {
        const safePage = relativeContained(dir, rawPage, 'fallback.config.page_unsafe', source);
        if (!safePage) continue;
        const page = changeExtension(safePage);
        if (!pages.includes(page)) pages.push(page);
    }
    return pages;
}

function sanitizeSubPackages(rawSubPackages, pages, dir, source) {
    if (!Array.isArray(rawSubPackages)) return [];
    const output = [];
    for (const rawSubPackage of rawSubPackages) {
        if (!rawSubPackage || typeof rawSubPackage !== 'object') {
            diagnostics.partial('fallback.config.subpackage_invalid', 'Skipped a non-object subpackage entry.', source);
            continue;
        }
        const root = relativeContained(
            dir,
            rawSubPackage.root,
            'fallback.config.subpackage_root_unsafe',
            source
        );
        if (!root) continue;
        const rootPrefix = root.endsWith('/') ? root : root + '/';
        const subPages = [];
        if (!Array.isArray(rawSubPackage.pages)) {
            diagnostics.partial('fallback.config.subpackage_pages_invalid', `Subpackage ${root} pages is not an array.`, source);
        } else {
            for (const rawPage of rawSubPackage.pages) {
                if (typeof rawPage !== 'string') {
                    diagnostics.partial('fallback.config.subpackage_page_invalid', `Skipped a non-string page in subpackage ${root}.`, source);
                    continue;
                }
                const normalizedPage = rawPage.replace(/\\/g, '/').replace(/^\/+/, '');
                const combinedCandidate = normalizedPage === root || normalizedPage.startsWith(rootPrefix)
                    ? normalizedPage
                    : path.posix.join(root, normalizedPage);
                const combined = relativeContained(
                    dir,
                    combinedCandidate,
                    'fallback.config.subpackage_page_unsafe',
                    source
                );
                if (!combined) continue;
                if (!combined.startsWith(rootPrefix)) {
                    diagnostics.partial(
                        'fallback.config.subpackage_page_outside_root',
                        `Skipped subpackage page outside ${root}: ${JSON.stringify(rawPage)}`,
                        source
                    );
                    continue;
                }
                const item = changeExtension(combined.slice(rootPrefix.length));
                if (!item) continue;
                if (!subPages.includes(item)) subPages.push(item);
                const mainIndex = pages.indexOf(changeExtension(combined));
                if (mainIndex !== -1) pages.splice(mainIndex, 1);
            }
        }
        output.push({...rawSubPackage, root: rootPrefix, pages: subPages});
    }
    return output;
}

function sanitizePageConfigs(rawPages, dir, source) {
    const pageConfigs = Object.create(null);
    if (!rawPages || typeof rawPages !== 'object' || Array.isArray(rawPages)) return pageConfigs;
    for (const [rawName, pageConfig] of Object.entries(rawPages)) {
        const safeName = relativeContained(dir, rawName, 'fallback.config.page_config_unsafe', source);
        if (!safeName) continue;
        pageConfigs[safeName] = pageConfig && typeof pageConfig === 'object' ? pageConfig : {};
    }

    // Component paths are package data too. Resolve relative components against
    // their declaring page, and root-relative components against the package.
    for (const [pageName, pageConfig] of Object.entries({...pageConfigs})) {
        const components = pageConfig.window && pageConfig.window.usingComponents;
        if (!components || typeof components !== 'object') continue;
        for (const componentPath of Object.values(components)) {
            if (typeof componentPath !== 'string') continue;
            const htmlPath = changeExtension(componentPath, '.html');
            const candidate = packageJoin(pageName, htmlPath);
            const safeComponent = relativeContained(
                dir,
                candidate,
                'fallback.config.component_path_unsafe',
                source
            );
            if (!safeComponent) continue;
            if (!pageConfigs[safeComponent]) pageConfigs[safeComponent] = {};
            if (!pageConfigs[safeComponent].window) pageConfigs[safeComponent].window = {};
            pageConfigs[safeComponent].window.component = true;
        }
    }
    return pageConfigs;
}

function prepareConfig(rawConfig, dir, source) {
    const pages = sanitizeMainPages(rawConfig.pages, dir, source);
    const entryCandidate = typeof rawConfig.entryPagePath === 'string'
        ? relativeContained(dir, rawConfig.entryPagePath, 'fallback.config.entry_page_unsafe', source)
        : null;
    const entryPage = entryCandidate ? changeExtension(entryCandidate) : '';
    const entryIndex = pages.indexOf(entryPage);
    if (entryIndex >= 0) pages.splice(entryIndex, 1);
    if (entryPage) pages.unshift(entryPage);

    const subPackages = sanitizeSubPackages(rawConfig.subPackages, pages, dir, source);
    const app = {
        pages,
        window: rawConfig.global && rawConfig.global.window,
        tabBar: rawConfig.tabBar,
        networkTimeout: rawConfig.networkTimeout
    };
    if (subPackages.length > 0) app.subPackages = subPackages;
    if (rawConfig.navigateToMiniProgramAppIdList) app.navigateToMiniProgramAppIdList = rawConfig.navigateToMiniProgramAppIdList;
    if (typeof rawConfig.debug !== 'undefined') app.debug = rawConfig.debug;

    if (app.tabBar && Array.isArray(app.tabBar.list)) {
        app.tabBar = {...app.tabBar, list: app.tabBar.list.flatMap(item => {
            if (!item || typeof item !== 'object') return [];
            const pagePath = relativeContained(
                dir,
                item.pagePath,
                'fallback.config.tabbar_page_unsafe',
                source
            );
            return pagePath ? [{...item, pagePath: changeExtension(pagePath)}] : [];
        })};
    }

    return {
        app,
        pageConfigs: sanitizePageConfigs(rawConfig.page, dir, source)
    };
}

function recoverAttachedPageConfigs(serviceCode, pageConfigs, dir, source) {
    const attachInfo = Object.create(null);
    try {
        const ast = parseScript(serviceCode);
        walk(ast, node => {
            if (node.type !== 'AssignmentExpression' || node.operator !== '=') return;
            const left = node.left;
            if (left.type !== 'MemberExpression' || left.object.type !== 'Identifier' || left.object.name !== '__wxAppCode__') return;
            const key = propertyName(left);
            if (!key.endsWith('.json')) return;
            const safeName = relativeContained(
                dir,
                changeExtension(key, '.html'),
                'fallback.page_config.assignment_path_unsafe',
                source
            );
            if (safeName) attachInfo[safeName] = staticEvaluate(node.right);
        });
    } catch (error) {
        diagnostics.partial('fallback.page_config.static_parse_failed', `Unable to recover page config assignments statically: ${error.message}`, source);
        console.warn('Unable to recover page config assignments statically:', error.message);
    }
    Object.assign(pageConfigs, attachInfo);
}

function savePageConfigs(pageConfigs, dir, configFile) {
    let deleteWeight = 8;
    for (const [pageName, pageConfig] of Object.entries(pageConfigs)) {
        const output = resolveContained(
            dir,
            changeExtension(pageName, '.json'),
            'fallback.config.page_config_output_unsafe',
            configFile
        );
        if (!output) continue;
        wu.save(output, JSON.stringify(pageConfig.window || {}, null, 4));
        if (path.resolve(configFile) === output) deleteWeight = 0;
    }
    return deleteWeight;
}

function saveSubPackagePlaceholders(app, dir, source) {
    if (!Array.isArray(app.subPackages)) return;
    for (const subPackage of app.subPackages) {
        for (const item of subPackage.pages || []) {
            const base = path.posix.join(subPackage.root, item);
            const outputs = [
                ['.js', `// ${changeExtension(base, '.js')}\nPage({data: {}})`],
                ['.wxml', `<!--${changeExtension(base, '.wxml')}--><text>${changeExtension(base, '.wxml')}</text>`],
                ['.wxss', `/* ${changeExtension(base, '.wxss')} */`]
            ];
            for (const [extension, content] of outputs) {
                const output = resolveContained(
                    dir,
                    changeExtension(base, extension),
                    'fallback.config.subpackage_output_unsafe',
                    source
                );
                if (output) wu.save(output, content);
            }
        }
    }
}

function doConfig(configFile, cb) {
    const dir = path.dirname(configFile);
    wu.get(configFile, content => {
        const rawConfig = JSON.parse(content);
        const {app, pageConfigs} = prepareConfig(rawConfig, dir, configFile);

        const workersFile = path.resolve(dir, 'workers.js');
        if (fs.existsSync(workersFile)) app.workers = getWorkerPath(workersFile);
        if (rawConfig.extAppid) {
            const extFile = resolveContained(dir, 'ext.json', 'fallback.config.ext_output_unsafe', configFile);
            if (extFile) wu.save(extFile, JSON.stringify({
                extEnable: true,
                extAppid: rawConfig.extAppid,
                ext: rawConfig.ext
            }, null, 4));
        }

        const serviceFile = path.resolve(dir, 'app-service.js');
        if (fs.existsSync(serviceFile)) {
            recoverAttachedPageConfigs(fs.readFileSync(serviceFile, {encoding: 'utf8'}), pageConfigs, dir, serviceFile);
        }

        const deleteWeight = savePageConfigs(pageConfigs, dir, configFile);
        saveSubPackagePlaceholders(app, dir, configFile);

        const saveAppAndFinish = () => {
            const appFile = resolveContained(dir, 'app.json', 'fallback.config.app_output_unsafe', configFile);
            if (appFile) wu.save(appFile, JSON.stringify(app, null, 4));
            cb({[configFile]: deleteWeight});
        };

        if (app.tabBar && Array.isArray(app.tabBar.list)) {
            wu.scanDirByExt(dir, '', files => {
                const digests = [];
                const digestEvent = new wu.CntEvent();
                const root = path.resolve(dir);
                const finish = () => {
                    for (const item of app.tabBar.list) {
                        if (item.iconData) {
                            const hash = crypto.createHash('MD5').update(item.iconData, 'base64').digest();
                            for (const [digest, filename] of digests) if (hash.equals(digest)) {
                                delete item.iconData;
                                item.iconPath = path.relative(root, filename).split(path.sep).join('/');
                                break;
                            }
                        }
                        if (item.selectedIconData) {
                            const hash = crypto.createHash('MD5').update(item.selectedIconData, 'base64').digest();
                            for (const [digest, filename] of digests) if (hash.equals(digest)) {
                                delete item.selectedIconData;
                                item.selectedIconPath = path.relative(root, filename).split(path.sep).join('/');
                                break;
                            }
                        }
                    }
                    saveAppAndFinish();
                };
                for (const filename of files) {
                    digestEvent.encount();
                    wu.get(filename, data => {
                        digests.push([crypto.createHash('MD5').update(data).digest(), filename]);
                        digestEvent.decount();
                    }, {});
                }
                digestEvent.check(finish);
            });
        } else {
            saveAppAndFinish();
        }
    });
}

module.exports = {
    changeExtension,
    doConfig,
    prepareConfig,
    sanitizePageConfigs,
    sanitizeSubPackages
};

if (require.main === module) {
    wu.commandExecute(doConfig, 'Split and make up weapp app-config.json file.\n\n<files...>\n\n<files...> app-config.json files to split and make up.');
}
