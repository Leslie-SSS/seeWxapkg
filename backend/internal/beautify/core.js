const prettier = require('prettier');
const { css: cssBeautify, html: htmlBeautify, js: jsBeautify } = require('js-beautify');
const babel = require('@babel/core');

const deobfuscatePlugin = require('./plugins/deobfuscate');
const wechatPlugin = require('./plugins/wechat');

const DEFAULT_MAX_CONTENT_SIZE = parseInt(process.env.MAX_CONTENT_SIZE || '512000', 10);
const DEFAULT_DEOBFUSCATE_ENABLED = process.env.DEOBFUSCATE_ENABLED !== 'false';

function getRuntimeConfig(overrides = {}) {
  return {
    deobfuscateEnabled: DEFAULT_DEOBFUSCATE_ENABLED,
    maxContentSize: DEFAULT_MAX_CONTENT_SIZE,
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
  const plugins = [wechatPlugin];
  if (config.deobfuscateEnabled) {
    plugins.push(deobfuscatePlugin);
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
    plugins,
    sourceType: 'unambiguous',
  });

  return result?.code || content;
}

async function beautifyJS(content, filename = '', config = getRuntimeConfig()) {
  let processedContent = content;
  const warnings = [];

  try {
    processedContent = transformJavaScript(content, config);
  } catch (error) {
    warnings.push(`AST transform skipped: ${error.message}`);
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

    return {
      success: true,
      content: formatted,
      warning: mergeWarnings(warnings),
    };
  } catch (error) {
    try {
      const fallback = jsBeautify(processedContent, {
        brace_style: 'collapse',
        break_chained_methods: false,
        comma_first: false,
        end_with_newline: true,
        indent_char: ' ',
        indent_size: 2,
        jslint_happy: false,
        keep_array_indentation: false,
        max_preserve_newlines: 2,
        operator_position: 'before-newline',
        preserve_newlines: true,
        space_before_conditional: true,
        unescape_strings: false,
        wrap_line_length: 100,
      });

      warnings.push(`Used fallback formatter due to: ${error.message}`);
      return {
        success: true,
        content: fallback,
        warning: mergeWarnings(warnings),
      };
    } catch (fallbackError) {
      warnings.push(`Formatting failed: ${fallbackError.message}`);
      return {
        success: true,
        content,
        warning: mergeWarnings(warnings),
      };
    }
  }
}

async function beautifyHTML(content) {
  if (hasUnsafeControlChars(content)) {
    return {
      success: true,
      content,
      warning: 'HTML formatting skipped due to control characters in content',
    };
  }

  try {
    const formatted = await prettier.format(content, {
      bracketSameLine: false,
      endOfLine: 'lf',
      htmlWhitespaceSensitivity: 'ignore',
      parser: 'html',
      printWidth: 120,
      proseWrap: 'preserve',
      tabWidth: 2,
    });

    return {
      success: true,
      content: formatted,
    };
  } catch (error) {
    try {
      const fallback = htmlBeautify(content, {
        content_unformatted: ['pre', 'code'],
        end_with_newline: true,
        extra_liners: ['view', 'text', 'image', 'scroll-view', 'swiper', 'swiper-item'],
        indent_char: ' ',
        indent_size: 2,
        max_char: 120,
        preserve_newlines: false,
        unformatted: ['code', 'pre', 'script', 'style'],
        wrap_attributes: 'auto',
        wrap_attributes_indent_size: 2,
        wrap_line_length: 120,
      });

      return {
        success: true,
        content: fallback,
        warning: `Used fallback formatter due to: ${error.message}`,
      };
    } catch (fallbackError) {
      return {
        success: true,
        content,
        warning: `HTML formatting failed: ${fallbackError.message}`,
      };
    }
  }
}

async function beautifyCSS(content) {
  if (hasUnsafeControlChars(content)) {
    return {
      success: true,
      content,
      warning: 'CSS formatting skipped due to control characters in content',
    };
  }

  try {
    const formatted = await prettier.format(content, {
      endOfLine: 'lf',
      parser: 'css',
      printWidth: 100,
      singleQuote: true,
      tabWidth: 2,
    });

    return {
      success: true,
      content: formatted,
    };
  } catch (error) {
    try {
      const fallback = cssBeautify(content, {
        end_with_newline: true,
        indent_char: ' ',
        indent_size: 2,
        newline_between_rules: true,
        preserve_newlines: false,
        selector_separator_newline: true,
        space_around_combinator: true,
      });

      return {
        success: true,
        content: fallback,
        warning: `Used fallback formatter due to: ${error.message}`,
      };
    } catch (fallbackError) {
      return {
        success: true,
        content,
        warning: `CSS formatting failed: ${fallbackError.message}`,
      };
    }
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
      return {
        success: true,
        content,
      };
    default: {
      const detectedType = detectFileType(content, filename);
      if (detectedType !== 'unknown') {
        return beautify(content, detectedType, filename, config);
      }

      return {
        success: true,
        content,
      };
    }
  }
}

module.exports = {
  beautify,
  beautifyCSS,
  beautifyHTML,
  beautifyJS,
  detectFileType,
  getRuntimeConfig,
  transformJavaScript,
};
