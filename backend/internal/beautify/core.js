const prettier = require('prettier');
const babel = require('@babel/core');

const deobfuscatePlugin = require('./plugins/deobfuscate');
const wechatPlugin = require('./plugins/wechat');

const DEFAULT_MAX_CONTENT_SIZE = parsePositiveInteger(process.env.MAX_CONTENT_SIZE, 512000);
const DEFAULT_DEOBFUSCATE_ENABLED = process.env.DEOBFUSCATE_ENABLED === 'true';
const DEFAULT_JOB_TIMEOUT_MS = parsePositiveInteger(process.env.BEAUTIFY_JOB_TIMEOUT_MS, 4000);
const DEFAULT_QUEUE_SIZE = parsePositiveInteger(process.env.BEAUTIFY_QUEUE_SIZE, 32);
const DEFAULT_WORKER_COUNT = parsePositiveInteger(process.env.BEAUTIFY_WORKERS, 2);

function parsePositiveInteger(value, fallback) {
  const parsed = Number.parseInt(value || '', 10);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : fallback;
}

function getRuntimeConfig(overrides = {}) {
  return {
    deobfuscateEnabled: DEFAULT_DEOBFUSCATE_ENABLED,
    jobTimeoutMs: DEFAULT_JOB_TIMEOUT_MS,
    maxContentSize: DEFAULT_MAX_CONTENT_SIZE,
    queueSize: DEFAULT_QUEUE_SIZE,
    workerCount: DEFAULT_WORKER_COUNT,
    ...overrides,
  };
}

function detectFileType(content, filename = '') {
  const ext = filename.split('.').pop()?.toLowerCase();

  if (ext === 'wxs' || ext === 'js') return 'javascript';
  if (ext === 'wxml' || ext === 'html') return 'html';
  if (ext === 'wxss' || ext === 'css') return 'css';
  if (ext === 'json') return 'json';

  const trimmed = content.trim();
  if (trimmed.startsWith('{') || trimmed.startsWith('[')) return 'json';
  if (trimmed.startsWith('<')) return 'html';
  if (
    trimmed.includes('function') ||
    trimmed.includes('const ') ||
    trimmed.includes('let ') ||
    trimmed.includes('var ')
  ) {
    return 'javascript';
  }

  return 'unknown';
}

function mergeWarnings(warnings) {
  const filtered = warnings.filter(Boolean);
  return filtered.length > 0 ? filtered.join('; ') : undefined;
}

function createResult(status, content, options = {}) {
  return {
    // `success` remains true when the original content is safely returned. This
    // preserves the existing Go protocol; callers that need fidelity details
    // should inspect `status`.
    success: true,
    status,
    content,
    ...(options.formatter ? { formatter: options.formatter } : {}),
    ...(options.warning ? { warning: options.warning } : {}),
  };
}

function formattedResult(original, formatted, options = {}) {
  return createResult(formatted === original ? 'unchanged' : 'formatted', formatted, options);
}

function hasUnsafeControlChars(content) {
  for (const char of content) {
    const code = char.charCodeAt(0);
    if (code < 0x20 && code !== 0x09 && code !== 0x0a && code !== 0x0d) {
      return true;
    }
  }

  return false;
}

function transformJavaScript(content, config) {
  if (!config.deobfuscateEnabled) {
    return content;
  }

  const result = babel.transformSync(content, {
    ast: false,
    babelrc: false,
    comments: true,
    compact: false,
    configFile: false,
    generatorOpts: {
      comments: true,
      compact: false,
      concise: false,
      retainLines: false,
    },
    parserOpts: {
      allowReturnOutsideFunction: true,
      errorRecovery: true,
      plugins: [
        'classProperties',
        'dynamicImport',
        'jsx',
        'nullishCoalescingOperator',
        'numericSeparator',
        'objectRestSpread',
        'optionalChaining',
        'topLevelAwait',
      ],
      sourceType: 'unambiguous',
    },
    plugins: [wechatPlugin, deobfuscatePlugin],
    sourceType: 'unambiguous',
  });

  return result?.code || content;
}

async function beautifyJS(content, filename = '', config = getRuntimeConfig()) {
  let processedContent = content;
  const warnings = [];

  if (config.deobfuscateEnabled) {
    try {
      processedContent = transformJavaScript(content, config);
    } catch (error) {
      warnings.push(`Optional AST readability pass skipped: ${error.message}`);
      processedContent = content;
    }
  }

  try {
    const formatted = await prettier.format(processedContent, {
      arrowParens: 'avoid',
      bracketSpacing: true,
      endOfLine: 'lf',
      parser: 'babel',
      printWidth: 100,
      semi: true,
      singleQuote: true,
      trailingComma: 'es5',
    });

    return formattedResult(content, formatted, {
      formatter: config.deobfuscateEnabled ? 'babel+prettier' : 'prettier',
      warning: mergeWarnings(warnings),
    });
  } catch (error) {
    warnings.push(`JavaScript formatting failed; original content preserved: ${error.message}`);
    return createResult('failed', content, { warning: mergeWarnings(warnings) });
  }
}

function findMarkupTagEnd(content, start) {
  if (content.startsWith('<!--', start)) {
    const end = content.indexOf('-->', start + 4);
    return end === -1 ? -1 : end + 3;
  }
  if (content.startsWith('<![CDATA[', start)) {
    const end = content.indexOf(']]>', start + 9);
    return end === -1 ? -1 : end + 3;
  }

  let quote = null;
  let moustacheDepth = 0;
  for (let index = start + 1; index < content.length; index += 1) {
    const char = content[index];
    const next = content[index + 1];
    if (quote) {
      if (char === quote && content[index - 1] !== '\\') quote = null;
      continue;
    }
    if (char === '"' || char === "'") {
      quote = char;
      continue;
    }
    if (char === '{' && next === '{') {
      moustacheDepth += 1;
      index += 1;
      continue;
    }
    if (char === '}' && next === '}' && moustacheDepth > 0) {
      moustacheDepth -= 1;
      index += 1;
      continue;
    }
    if (char === '>' && moustacheDepth === 0) return index + 1;
  }
  return -1;
}

function tokenizeTagBody(body) {
  const tokens = [];
  let tokenStart = -1;
  let quote = null;
  let moustacheDepth = 0;

  const push = end => {
    if (tokenStart !== -1) tokens.push(body.slice(tokenStart, end));
    tokenStart = -1;
  };

  for (let index = 0; index < body.length; index += 1) {
    const char = body[index];
    const next = body[index + 1];
    if (quote) {
      if (char === quote && body[index - 1] !== '\\') quote = null;
      continue;
    }
    if (char === '"' || char === "'") {
      if (tokenStart === -1) tokenStart = index;
      quote = char;
      continue;
    }
    if (char === '{' && next === '{') {
      if (tokenStart === -1) tokenStart = index;
      moustacheDepth += 1;
      index += 1;
      continue;
    }
    if (char === '}' && next === '}' && moustacheDepth > 0) {
      moustacheDepth -= 1;
      index += 1;
      continue;
    }
    if (/\s/.test(char) && moustacheDepth === 0) {
      push(index);
      continue;
    }
    if (tokenStart === -1) tokenStart = index;
  }
  push(body.length);
  return tokens;
}

function formatMarkupTag(rawTag, printWidth = 120) {
  if (
    rawTag.startsWith('<!--') ||
    rawTag.startsWith('<![CDATA[') ||
    rawTag.startsWith('<!') ||
    rawTag.startsWith('<?') ||
    rawTag.startsWith('</')
  ) {
    return rawTag;
  }

  const selfClosing = /\/\s*>$/.test(rawTag);
  const body = rawTag.slice(1, selfClosing ? rawTag.lastIndexOf('/') : -1).trim();
  const tokens = tokenizeTagBody(body);
  if (tokens.length === 0 || !/^[A-Za-z][\w:.-]*$/.test(tokens[0])) return rawTag;

  const suffix = selfClosing ? ' />' : '>';
  const singleLine = `<${tokens.join(' ')}${suffix}`;
  if (tokens.length === 1 || singleLine.length <= printWidth) return singleLine;

  return `<${tokens[0]}\n${tokens.slice(1).map(token => `  ${token}`).join('\n')}\n${suffix.trimStart()}`;
}

/**
 * Format only markup syntax and preserve every byte outside tags. In particular,
 * this never inserts indentation text nodes between WXML elements and never
 * rewrites inline WXS source.
 */
function formatWXMLTags(content, printWidth = 120) {
  let result = '';
  let cursor = 0;

  while (cursor < content.length) {
    const start = content.indexOf('<', cursor);
    if (start === -1) {
      result += content.slice(cursor);
      break;
    }
    result += content.slice(cursor, start);

    const plausibleTag = content.slice(start).match(/^<\/?[A-Za-z][\w:.-]*/);
    const specialTag = content.startsWith('<!--', start) ||
      content.startsWith('<![CDATA[', start) ||
      content.startsWith('<!', start) ||
      content.startsWith('<?', start);
    if (!plausibleTag && !specialTag) {
      result += '<';
      cursor = start + 1;
      continue;
    }

    const end = findMarkupTagEnd(content, start);
    if (end === -1) {
      result += content.slice(start);
      break;
    }
    const rawTag = content.slice(start, end);
    result += formatMarkupTag(rawTag, printWidth);
    cursor = end;

    if (/^<wxs(?:\s|>)/i.test(rawTag) && !/\/\s*>$/.test(rawTag)) {
      const closingStart = content.toLowerCase().indexOf('</wxs', cursor);
      if (closingStart !== -1) {
        const closingEnd = findMarkupTagEnd(content, closingStart);
        if (closingEnd !== -1) {
          result += content.slice(cursor, closingEnd);
          cursor = closingEnd;
        }
      }
    }
  }

  return result;
}

async function beautifyHTML(content) {
  if (hasUnsafeControlChars(content)) {
    return createResult('skipped', content, {
      warning: 'WXML formatting skipped due to control characters in content',
    });
  }

  try {
    const formatted = formatWXMLTags(content);
    return formattedResult(content, formatted, { formatter: 'wxml-safe' });
  } catch (error) {
    return createResult('failed', content, {
      warning: `WXML formatting failed; original content preserved: ${error.message}`,
    });
  }
}

async function beautifyCSS(content) {
  if (hasUnsafeControlChars(content)) {
    return createResult('skipped', content, {
      warning: 'CSS formatting skipped due to control characters in content',
    });
  }

  try {
    const formatted = await prettier.format(content, {
      endOfLine: 'lf',
      parser: 'css',
      printWidth: 100,
      singleQuote: true,
      tabWidth: 2,
    });

    return formattedResult(content, formatted, { formatter: 'prettier' });
  } catch (error) {
    return createResult('failed', content, {
      warning: `CSS formatting failed; original content preserved: ${error.message}`,
    });
  }
}

async function beautify(content, type, filename = '', config = getRuntimeConfig()) {
  switch (type) {
    case 'javascript':
    case 'js':
    case 'wxs':
      return beautifyJS(content, filename, config);
    case 'html':
    case 'wxml':
      return beautifyHTML(content);
    case 'css':
    case 'wxss':
      return beautifyCSS(content);
    case 'json':
      return createResult('unchanged', content);
    default: {
      const detectedType = detectFileType(content, filename);
      if (detectedType !== 'unknown') {
        return beautify(content, detectedType, filename, config);
      }

      return createResult('skipped', content, { warning: 'Unknown file type; content preserved' });
    }
  }
}

module.exports = {
  beautify,
  beautifyCSS,
  beautifyHTML,
  beautifyJS,
  createResult,
  getRuntimeConfig,
};
