const wu = require("./wuLib.js");
const wuJs = require("./wuJs.js");
const wuCfg = require("./wuConfig.js");
const wuMl = require("./wuWxml.js");
const wuSs = require("./wuWxss.js");
const path = require("path");
const fs = require("fs");
const {TextDecoder} = require('util');
const diagnostics = require('./wuDiagnostics.js');
const {safeResolve} = require('./wuStatic.js');

function boundedEnvironmentInteger(name, fallback, hardMaximum) {
    const parsed = Number.parseInt(process.env[name] || '', 10);
    if (!Number.isSafeInteger(parsed) || parsed <= 0) return fallback;
    return Math.min(parsed, hardMaximum);
}

const LIMITS = Object.freeze({
    maxFileCount: boundedEnvironmentInteger('WXAPKG_MAX_FILE_COUNT', 20000, 100000),
    maxFileSize: boundedEnvironmentInteger('WXAPKG_MAX_FILE_SIZE', 128 * 1024 * 1024, 512 * 1024 * 1024),
    maxIndexSize: boundedEnvironmentInteger('WXAPKG_MAX_INDEX_SIZE', 32 * 1024 * 1024, 128 * 1024 * 1024),
    maxNameBytes: boundedEnvironmentInteger('WXAPKG_MAX_NAME_BYTES', 4096, 65536),
    maxPackageSize: boundedEnvironmentInteger('WXAPKG_MAX_PACKAGE_SIZE', 256 * 1024 * 1024, 1024 * 1024 * 1024),
    maxTotalExtractedSize: boundedEnvironmentInteger('WXAPKG_MAX_EXTRACTED_SIZE', 512 * 1024 * 1024, 1024 * 1024 * 1024)
});

const utf8Decoder = new TextDecoder('utf-8', {fatal: true});

function ensureAvailable(buffer, offset, length, label) {
    if (!Number.isSafeInteger(offset) || !Number.isSafeInteger(length) || offset < 0 || length < 0 || offset > buffer.length || length > buffer.length - offset) {
        throw new Error(`Invalid wxapkg index: ${label} exceeds buffer bounds`);
    }
}

function header(buf) {
    if (!Buffer.isBuffer(buf)) throw new Error('Invalid wxapkg: expected a Buffer');
    if (buf.length < 14) throw new Error('Invalid wxapkg: truncated header');
    if (buf.length > LIMITS.maxPackageSize) throw new Error(`Invalid wxapkg: package exceeds ${LIMITS.maxPackageSize} bytes`);
    console.log("\nHeader info:");
    let firstMark = buf.readUInt8(0);
    console.log("  firstMark: 0x%s", firstMark.toString(16));
    let unknownInfo = buf.readUInt32BE(1);
    console.log("  unknownInfo: ", unknownInfo);
    let infoListLength = buf.readUInt32BE(5);
    console.log("  infoListLength: ", infoListLength);
    let dataLength = buf.readUInt32BE(9);
    console.log("  dataLength: ", dataLength);
    let lastMark = buf.readUInt8(13);
    console.log("  lastMark: 0x%s", lastMark.toString(16));
    if (firstMark != 0xbe || lastMark != 0xed) throw Error("Magic number is not correct!");
    if (infoListLength < 4 || infoListLength > LIMITS.maxIndexSize) {
        throw new Error(`Invalid wxapkg: index length ${infoListLength} is outside the allowed range`);
    }
    const dataStart = 14 + infoListLength;
    if (!Number.isSafeInteger(dataStart) || dataStart > buf.length) {
        throw new Error('Invalid wxapkg: index extends beyond package');
    }
    if (dataLength > buf.length - dataStart) {
        throw new Error('Invalid wxapkg: declared data length extends beyond package');
    }
    const dataEnd = dataStart + dataLength;
    if (dataEnd !== buf.length) {
        throw new Error(`Invalid wxapkg: declared size ${dataEnd} does not match package size ${buf.length}`);
    }
    return {dataEnd, dataLength, dataStart, infoListLength};
}

function genList(buf, dataStart, dataEnd) {
    if (!Buffer.isBuffer(buf) || buf.length < 4) throw new Error('Invalid wxapkg index: truncated file count');
    if (!Number.isSafeInteger(dataStart) || !Number.isSafeInteger(dataEnd) || dataStart < 0 || dataEnd < dataStart) {
        throw new Error('Invalid wxapkg index: invalid data bounds');
    }
    console.log("\nFile list info:");
    let fileCount = buf.readUInt32BE(0);
    console.log("  fileCount: ", fileCount);
    if (fileCount > LIMITS.maxFileCount) {
        throw new Error(`Invalid wxapkg index: file count ${fileCount} exceeds ${LIMITS.maxFileCount}`);
    }
    if (fileCount > Math.floor((buf.length - 4) / 12)) {
        throw new Error('Invalid wxapkg index: file count cannot fit in index');
    }
    let fileInfo = [], off = 4;
    let totalExtractedSize = 0;
    const names = new Set();
    for (let i = 0; i < fileCount; i++) {
        let info = {};
        ensureAvailable(buf, off, 4, `file ${i} name length`);
        let nameLen = buf.readUInt32BE(off);
        off += 4;
        if (nameLen === 0 || nameLen > LIMITS.maxNameBytes) {
            throw new Error(`Invalid wxapkg index: file ${i} name length ${nameLen} is outside the allowed range`);
        }
        ensureAvailable(buf, off, nameLen, `file ${i} name`);
        try {
            info.name = utf8Decoder.decode(buf.subarray(off, off + nameLen));
        } catch (error) {
            throw new Error(`Invalid wxapkg index: file ${i} name is not valid UTF-8`);
        }
        if (info.name.includes('\0')) throw new Error(`Invalid wxapkg index: file ${i} name contains NUL`);
        // Validate traversal/absolute-path handling before any filesystem write.
        const validationRoot = '/__seewx_wxapkg_root__';
        const validatedPath = safeResolve(validationRoot, info.name);
        const normalizedName = path.relative(validationRoot, validatedPath).split(path.sep).join('/');
        if (!normalizedName || normalizedName === '.') {
            throw new Error(`Invalid wxapkg index: file ${i} has an empty output path`);
        }
        if (names.has(normalizedName)) throw new Error(`Invalid wxapkg index: duplicate file name ${info.name}`);
        names.add(normalizedName);
        off += nameLen;
        ensureAvailable(buf, off, 8, `file ${i} offset and size`);
        info.off = buf.readUInt32BE(off);
        off += 4;
        info.size = buf.readUInt32BE(off);
        off += 4;
        if (info.size > LIMITS.maxFileSize) {
            throw new Error(`Invalid wxapkg index: file ${i} exceeds ${LIMITS.maxFileSize} bytes`);
        }
        if (info.off < dataStart || info.off > dataEnd || info.size > dataEnd - info.off) {
            throw new Error(`Invalid wxapkg index: file ${i} data range is outside package data`);
        }
        totalExtractedSize += info.size;
        if (!Number.isSafeInteger(totalExtractedSize) || totalExtractedSize > LIMITS.maxTotalExtractedSize) {
            throw new Error(`Invalid wxapkg index: total extracted size exceeds ${LIMITS.maxTotalExtractedSize} bytes`);
        }
        fileInfo.push(info);
    }
    if (off !== buf.length) throw new Error(`Invalid wxapkg index: ${buf.length - off} trailing byte(s)`);
    return fileInfo;
}

function saveFile(dir, buf, list) {
    console.log("Saving files...");
    fs.mkdirSync(dir, {recursive: true, mode: 0o700});
    for (let info of list) {
        const output = safeResolve(dir, info.name);
        fs.mkdirSync(path.dirname(output), {recursive: true, mode: 0o700});
        fs.writeFileSync(output, buf.subarray(info.off, info.off + info.size), {flag: 'wx', mode: 0o600});
    }
}

function packDone(dir, cb, order) {
    console.log("Unpack done.");
    let weappEvent = new wu.CntEvent, needDelete = {};
    const finishWeapp = () => {
        wu.addIO(() => {
            console.log("Split and make up done.");
            if (!order.includes("d")) {
                console.log("Delete files...");
                wu.addIO(() => console.log("Deleted.\n\nFile done."));
                for (let name in needDelete) if (needDelete[name] >= 8) wu.del(name);
            }
            diagnostics.writeReport(dir);
            cb();
        });
    };

    function doBack(deletable) {
        for (let key in deletable) {
            if (!needDelete[key]) needDelete[key] = 0;
            needDelete[key] += deletable[key];//all file have score bigger than 8 will be delete.
        }
        weappEvent.decount();
    }

    function dealThreeThings(dir, mainDir, nowDir) {
        console.log("Split app-service.js and make up configs & wxss & wxml & wxs...");

        //deal config
        if (fs.existsSync(path.resolve(dir, "app-config.json"))) {
            weappEvent.encount();
            wuCfg.doConfig(path.resolve(dir, "app-config.json"), doBack);
            console.log('deal config ok');
        }
        //deal js
        if (fs.existsSync(path.resolve(dir, "app-service.js"))) {
            weappEvent.encount();
            wuJs.splitJs(path.resolve(dir, "app-service.js"), doBack, mainDir);
            console.log('deal js ok');
        }
        if (fs.existsSync(path.resolve(dir, "workers.js"))) {
            weappEvent.encount();
            wuJs.splitJs(path.resolve(dir, "workers.js"), doBack, mainDir);
            console.log('deal js2 ok');
        }
        //deal html
        if (mainDir) {
            if (fs.existsSync(path.resolve(dir, "page-frame.js"))) {
                weappEvent.encount();
                wuMl.doFrame(path.resolve(dir, "page-frame.js"), doBack, order, mainDir);
                console.log('deal sub html ok');
            }
            weappEvent.encount();
            wuSs.doWxss(dir, doBack, mainDir, nowDir);
        } else {
            if (fs.existsSync(path.resolve(dir, "page-frame.html"))) {
                weappEvent.encount();
                wuMl.doFrame(path.resolve(dir, "page-frame.html"), doBack, order, mainDir);
                console.log('deal html ok');
            } else if (fs.existsSync(path.resolve(dir, "page-frame.js"))) {
                // WeChat 4.x main packages ship page-frame.js as the renderer
                // source; app-wxss.js may exist as a stylesheet-only bundle and
                // must not shadow the renderer registry, so page-frame.js takes
                // priority over it. Some 4.x builds ship a placeholder
                // page-frame.js with the real registry in app-wxss.js, so an
                // empty frame falls back to app-wxss.js.
                const framePath = path.resolve(dir, "page-frame.js");
                weappEvent.encount();
                wuMl.doFrame(framePath, (result) => {
                    doBack(result);
                    if (result && result.empty) {
                        const wxssPath = path.resolve(dir, "app-wxss.js");
                        if (fs.existsSync(wxssPath)) {
                            console.log('page-frame.js was a placeholder; falling back to app-wxss.js');
                            weappEvent.encount();
                            wuMl.doFrame(wxssPath, doBack, order, mainDir);
                        }
                    }
                }, order, mainDir);
                console.log('deal page-frame.js ok');
            } else if (fs.existsSync(path.resolve(dir, "app-wxss.js"))) {
                weappEvent.encount();
                wuMl.doFrame(path.resolve(dir, "app-wxss.js"), doBack, order, mainDir);
                if (!needDelete[path.resolve(dir, "page-frame.js")]) {
                    needDelete[path.resolve(dir, "page-frame.js")] = 8;
                }
                console.log('deal wxss.js ok');
            } else {
                throw Error("page-frame-like file is not found in the package by auto.");
            }
            //Force it run at last, becuase lots of error occured in this part
            weappEvent.encount();
            wuSs.doWxss(dir, doBack);

            console.log('deal css ok');
        }
        weappEvent.check(finishWeapp);
    }

//This will be the only func running this time, so async is needless.
    if (fs.existsSync(path.resolve(dir, "app-service.js"))) {
        //weapp
        dealThreeThings(dir);
    } else if (fs.existsSync(path.resolve(dir, "game.js"))) {
        //wegame
        console.log("Split game.js and rewrite game.json...");
        let gameCfg = path.resolve(dir, "app-config.json");
        wu.get(gameCfg, cfgPlain => {
            let cfg = JSON.parse(cfgPlain);
            if (cfg.subContext) {
                console.log("Found subContext, splitting it...")
                delete cfg.subContext;
                let contextPath = path.resolve(dir, "subContext.js");
                wuJs.splitJs(contextPath, () => wu.del(contextPath));
            }
            wu.save(path.resolve(dir, "game.json"), JSON.stringify(cfg, null, 4));
            wu.del(gameCfg);
        });
        wuJs.splitJs(path.resolve(dir, "game.js"), () => {
            wu.addIO(() => {
                console.log("Split and rewrite done.");
                diagnostics.writeReport(dir);
                cb();
            });
        });
    } else {//分包
        let doSubPkg = false;
        for (const orderElement of order) {
            if (orderElement.indexOf('s=') !== -1) {
                let mainDir = orderElement.substring(2, orderElement.length);
                console.log("now dir: " + dir);
                console.log("param of mainDir: " + mainDir);

                let findDir = function (dir, oldDir) {
                    let files = fs.readdirSync(dir);
                    for (const file of files) {
                        let workDir = path.join(dir, file);
                        const stats = fs.lstatSync(workDir);
                        if (stats.isSymbolicLink() || !stats.isDirectory()) continue;
                        if (fs.existsSync(path.resolve(workDir, "app-service.js"))) {
                            console.log("sub package word dir: " + workDir);
                            mainDir = path.resolve(oldDir, mainDir);
                            console.log("real mainDir: " + mainDir);
                            dealThreeThings(workDir, mainDir, oldDir);
                            doSubPkg = true;
                            return true;
                        } else {
                            if (findDir(workDir, oldDir)) return true;
                        }
                    }
                    return false;
                };

                findDir(dir, dir);

            }
        }
        if (!doSubPkg) {
            throw new Error("检测到此包是分包后的子包, 请通过 -s 参数指定存放路径后重试, 如 node wuWxapkg.js -s=/xxx/xxx ./testpkg/test-pkg-sub.wxapkg");
        }
    }
}

function doFile(name, cb, order) {
    for (let ord of order) if (ord.startsWith("s=")) global.subPack = ord.slice(3);
    console.log("Unpack file " + name + "...");
    let dir = path.resolve(name, "..", path.basename(name, ".wxapkg"));
    const packageStat = fs.statSync(name);
    if (!packageStat.isFile()) throw new Error('Invalid wxapkg input: expected a regular file');
    if (packageStat.size > LIMITS.maxPackageSize) {
        throw new Error(`Invalid wxapkg: package exceeds ${LIMITS.maxPackageSize} bytes`);
    }
    wu.get(name, buf => {
        const packageHeader = header(buf);
        const index = buf.subarray(14, 14 + packageHeader.infoListLength);
        const files = genList(index, packageHeader.dataStart, packageHeader.dataEnd);
        if (order.includes("o")) wu.addIO(console.log.bind(console), "Unpack done.");
        else wu.addIO(packDone, dir, cb, order);
        saveFile(dir, buf, files);
    }, {});
}

module.exports = {LIMITS, doFile, genList, header, saveFile};
if (require.main === module) {
    wu.commandExecute(doFile, "Unpack a wxapkg file.\n\n[-o] [-d] [-s=<Main Dir>] <files...>\n\n-d Do not delete transformed unpacked files.\n-o Do not execute any operation after unpack.\n-s=<Main Dir> Regard all packages provided as subPackages and\n              regard <Main Dir> as the directory of sources of the main package.\n<files...> wxapkg files to unpack");
}
