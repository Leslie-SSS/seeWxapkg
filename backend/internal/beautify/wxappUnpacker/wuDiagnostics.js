const fs = require('fs');
const path = require('path');

const diagnostics = [];
const diagnosticKeys = new Set();

function partial(code, message, file, metadata) {
    const key = `${code}\0${file || ''}`;
    if (diagnosticKeys.has(key)) return;
    diagnosticKeys.add(key);
    const item = {
        code,
        level: 'warning',
        message,
        status: 'partial'
    };
    if (file) item.file = file;
    if (metadata && typeof metadata === 'object' && !Array.isArray(metadata)) {
        item.metadata = metadata;
    }
    diagnostics.push(item);
    console.error('SEEWX_RECOVERY_DIAGNOSTIC ' + JSON.stringify(item));
}

function snapshot() {
    return {
        diagnostics: [...diagnostics],
        status: diagnostics.length > 0 ? 'partial' : 'completed'
    };
}

function writeReport(outputDir) {
    const report = snapshot();
    const destination = path.resolve(outputDir, '.seewx-recovery-status');
    fs.writeFileSync(destination, JSON.stringify(report, null, 2), {encoding: 'utf8', mode: 0o600});
    return destination;
}

module.exports = {
    partial,
    snapshot,
    writeReport
};
