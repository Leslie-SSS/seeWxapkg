const fs = require('fs');
const path = require('path');
const parser = require('@babel/parser');
const csstree = require('css-tree');
const parse5 = require('parse5');

function walk(dir, acc = []) {
  if (!fs.existsSync(dir)) return acc;
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const fullPath = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      walk(fullPath, acc);
      continue;
    }
    acc.push(fullPath);
  }
  return acc;
}

function toRel(root, file) {
  return path.relative(root, file).split(path.sep).join('/');
}

function parseJS(file) {
  const content = fs.readFileSync(file, 'utf8');
  parser.parse(content, {
    sourceType: 'unambiguous',
    plugins: [
      'jsx',
      'classProperties',
      'dynamicImport',
      'optionalChaining',
      'nullishCoalescingOperator',
      'objectRestSpread',
      'topLevelAwait',
    ],
  });
}

function parseWXSS(file) {
  const content = fs.readFileSync(file, 'utf8');
  csstree.parse(content, {
    positions: false,
    parseValue: true,
    parseRulePrelude: true,
    onParseError(error) {
      // css-tree normally recovers malformed selector preludes as Raw nodes.
      // Recovery output must not receive a perfect score when the selector is
      // not valid WXSS/CSS, so make those tolerant-parser errors explicit.
      throw error;
    },
  });
}

function scanWxmlTags(content) {
  const tags = [];
  for (let index = 0; index < content.length; index += 1) {
    if (content[index] !== '<') continue;
    if (content.startsWith('<!--', index)) {
      const commentEnd = content.indexOf('-->', index + 4);
      if (commentEnd < 0) throw new Error('unclosed WXML comment');
      index = commentEnd + 2;
      continue;
    }

    let cursor = index + 1;
    while (/\s/.test(content[cursor] || '')) cursor += 1;
    const closing = content[cursor] === '/';
    if (closing) {
      cursor += 1;
      while (/\s/.test(content[cursor] || '')) cursor += 1;
    }
    const nameMatch = content.slice(cursor).match(/^([A-Za-z][\w.:-]*)/);
    if (!nameMatch) continue;
    const name = nameMatch[1].toLowerCase();
    cursor += nameMatch[1].length;
    if (!/[\s/>]/.test(content[cursor] || '')) continue;

    let quote = '';
    let moustacheDepth = 0;
    let end = -1;
    for (let pos = cursor; pos < content.length; pos += 1) {
      const char = content[pos];
      if (quote) {
        if (char === quote && content[pos - 1] !== '\\') quote = '';
        continue;
      }
      if (char === '"' || char === "'") {
        quote = char;
        continue;
      }
      if (content.startsWith('{{', pos)) {
        moustacheDepth += 1;
        pos += 1;
        continue;
      }
      if (moustacheDepth > 0 && content.startsWith('}}', pos)) {
        moustacheDepth -= 1;
        pos += 1;
        continue;
      }
      if (moustacheDepth > 0) continue;
      if (char === '<') break;
      if (char === '>') {
        end = pos;
        break;
      }
    }
    if (end < 0) throw new Error(`unterminated tag <${name}>`);

    const attributes = content.slice(cursor, end);
    const selfClosing = /\/\s*$/.test(attributes);
    tags.push({ name, closing, selfClosing, attributes });
    index = end;

    // Inline WXS contains JavaScript rather than WXML. Skip it as raw text so
    // comparison operators and string literals cannot be mistaken for tags.
    if (!closing && !selfClosing && name === 'wxs') {
      const closeStart = content.toLowerCase().indexOf('</wxs', end + 1);
      if (closeStart >= 0) index = closeStart - 1;
    }
  }
  return tags;
}

function validateWXMLStructure(content) {
  const stack = [];
  for (const tag of scanWxmlTags(content)) {
    if (tag.closing) {
      const expected = stack.pop();
      if (expected !== tag.name) {
        throw new Error(`mismatched closing tag </${tag.name}>${expected ? `; expected </${expected}>` : ''}`);
      }
      continue;
    }
    if (!tag.selfClosing) stack.push(tag.name);
  }
  if (stack.length > 0) {
    throw new Error(`unclosed tag <${stack[stack.length - 1]}>`);
  }
}

function collectWxmlRefs(content) {
  const refs = [];
  for (const tag of scanWxmlTags(content)) {
    if (tag.closing || !['import', 'include', 'wxs'].includes(tag.name)) continue;
    const srcMatch = tag.attributes.match(/\bsrc\s*=\s*(["'])(.*?)\1/i);
    if (srcMatch && srcMatch[2]) {
      refs.push({ tag: tag.name, src: srcMatch[2] });
    }
  }
  return refs;
}

function resolveWxmlRef(file, root, ref) {
  if (/\{\{/.test(ref.src) || /^(?:[a-z]+:)?\/\//i.test(ref.src)) return null;
  const clean = ref.src.split(/[?#]/, 1)[0];
  const resolved = clean.startsWith('/')
    ? path.resolve(root, `.${clean}`)
    : path.resolve(path.dirname(file), clean);
  const relative = path.relative(root, resolved);
  if (relative === '..' || relative.startsWith(`..${path.sep}`) || path.isAbsolute(relative)) {
    return { path: resolved, outsideRoot: true };
  }
  const candidates = [resolved];
  if (!path.extname(resolved)) {
    candidates.push(`${resolved}.${ref.tag === 'wxs' ? 'wxs' : 'wxml'}`);
  }
  return { path: candidates.find((candidate) => fs.existsSync(candidate)) || resolved, outsideRoot: false };
}

function parseWXML(file, root) {
  const content = fs.readFileSync(file, 'utf8');
  validateWXMLStructure(content);
  // parse5 remains useful as a tolerant tokenization smoke-test, while the
  // WXML-aware structural check above prevents its HTML error recovery from
  // silently accepting broken tag nesting.
  parse5.parseFragment(content, { sourceCodeLocationInfo: false });
  const refs = collectWxmlRefs(content);
  const missing = [];
  for (const ref of refs) {
    const resolution = resolveWxmlRef(file, root, ref);
    if (resolution && (resolution.outsideRoot || !fs.existsSync(resolution.path))) {
      missing.push({
        file: toRel(root, file),
        tag: ref.tag,
        target: ref.src,
      });
    }
  }
  return missing;
}

function main() {
  const root = process.argv[2];
  if (!root) {
    throw new Error('usage: verify_artifacts.js <rootDir>');
  }

  const result = {
    jsFiles: 0,
    jsParseable: 0,
    jsErrors: [],
    wxmlFiles: 0,
    wxmlParseable: 0,
    wxmlErrors: [],
    wxmlMissingRefs: [],
    wxssFiles: 0,
    wxssParseable: 0,
    wxssErrors: [],
  };

  for (const file of walk(root)) {
    const rel = toRel(root, file);
    const ext = path.extname(file).toLowerCase();
    try {
      if (ext === '.js' || ext === '.wxs') {
        result.jsFiles += 1;
        parseJS(file);
        result.jsParseable += 1;
        continue;
      }
      if (ext === '.wxml') {
        result.wxmlFiles += 1;
        const missing = parseWXML(file, root);
        result.wxmlParseable += 1;
        result.wxmlMissingRefs.push(...missing);
        continue;
      }
      if (ext === '.wxss' || ext === '.css') {
        result.wxssFiles += 1;
        parseWXSS(file);
        result.wxssParseable += 1;
      }
    } catch (error) {
      const payload = {
        file: rel,
        error: error && error.message ? error.message : String(error),
      };
      if (ext === '.js' || ext === '.wxs') {
        result.jsErrors.push(payload);
        continue;
      }
      if (ext === '.wxml') {
        result.wxmlErrors.push(payload);
        continue;
      }
      if (ext === '.wxss' || ext === '.css') {
        result.wxssErrors.push(payload);
      }
    }
  }

  process.stdout.write(JSON.stringify(result));
}

main();
