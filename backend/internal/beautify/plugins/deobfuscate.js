/**
 * Conservative deobfuscation Babel plugin for WeChat mini-program bundles.
 *
 * The plugin only renames high-confidence bindings:
 * - lifecycle method params like onLoad(e) -> onLoad(options)
 * - array/promise callback params like map((e, n) => {}) -> (item, index)
 * - wx.request({ success(e) {} }) style callback params
 * - `var t = this` aliases inside WeChat definitions -> `var page = this`
 *
 * Variable declaration kinds are intentionally preserved. Replacing `var` with
 * `const` can introduce a temporal dead zone and change function/block scoping.
 */

function isMinifiedName(name) {
  return /^[a-z][0-9]?$/.test(name);
}

const LIFECYCLE_PARAMS = {
  onLoad: ['options', 'query'],
  onShow: [],
  onReady: [],
  onHide: [],
  onUnload: [],
  onPullDownRefresh: [],
  onReachBottom: [],
  onShareAppMessage: ['res', 'options'],
  onPageScroll: ['e', 'scrollTop'],
  onTabItemTap: ['item'],
  onResize: ['res'],
};

const ARRAY_METHOD_PATTERNS = {
  every: ['item', 'index', 'array'],
  filter: ['item', 'index', 'array'],
  find: ['item', 'index', 'array'],
  findIndex: ['item', 'index', 'array'],
  forEach: ['item', 'index', 'array'],
  map: ['item', 'index', 'array'],
  reduce: ['acc', 'item', 'index', 'array'],
  reduceRight: ['acc', 'item', 'index', 'array'],
  some: ['item', 'index', 'array'],
  sort: ['a', 'b'],
};

const PROMISE_PATTERNS = {
  catch: ['err'],
  finally: [],
  then: ['res'],
};

const WX_API_PATTERNS = {
  chooseImage: {
    fail: ['err'],
    success: ['res'],
  },
  getLocation: {
    fail: ['err'],
    success: ['res'],
  },
  getStorage: {
    fail: ['err'],
    success: ['res'],
  },
  navigateBack: {
    fail: ['err'],
    success: ['res'],
  },
  navigateTo: {
    fail: ['err'],
    success: ['res'],
  },
  request: {
    complete: ['res'],
    fail: ['err'],
    success: ['res'],
  },
  setStorage: {
    fail: ['err'],
    success: ['res'],
  },
  showModal: {
    fail: ['err'],
    success: ['res'],
  },
};

const WECHAT_ROOTS = new Set(['App', 'Behavior', 'Component', 'Page']);

function getKeyName(node) {
  if (!node) {
    return null;
  }

  if (node.type === 'Identifier') {
    return node.name;
  }

  if (node.type === 'StringLiteral') {
    return node.value;
  }

  return null;
}

function getFunctionKeyName(path) {
  if (path.isObjectMethod()) {
    return getKeyName(path.node.key);
  }

  if (path.parentPath?.isObjectProperty() && path.parentPath.get('value') === path) {
    return getKeyName(path.parentPath.node.key);
  }

  return null;
}

function findWeChatRoot(path) {
  let current = path;

  while (current) {
    if (current.isCallExpression()) {
      const calleeName = getKeyName(current.node.callee);
      if (WECHAT_ROOTS.has(calleeName)) {
        return {
          rootName: calleeName,
          path: current,
        };
      }
    }

    current = current.parentPath;
  }

  return null;
}

function getArraySuggestions(path) {
  const parent = path.parentPath;
  if (!parent?.isCallExpression()) {
    return null;
  }

  const callee = parent.node.callee;
  if (callee?.type !== 'MemberExpression') {
    return null;
  }

  const methodName = getKeyName(callee.property);
  return ARRAY_METHOD_PATTERNS[methodName] || null;
}

function getPromiseSuggestions(path) {
  const parent = path.parentPath;
  if (!parent?.isCallExpression()) {
    return null;
  }

  const callee = parent.node.callee;
  if (callee?.type !== 'MemberExpression') {
    return null;
  }

  const methodName = getKeyName(callee.property);
  return PROMISE_PATTERNS[methodName] || null;
}

function getWxApiSuggestions(path) {
  let callbackName;
  let objectPath;

  if (path.isObjectMethod()) {
    callbackName = getKeyName(path.node.key);
    objectPath = path.parentPath;
  } else {
    if (!path.parentPath?.isObjectProperty() || path.parentPath.get('value') !== path) {
      return null;
    }

    callbackName = getKeyName(path.parentPath.node.key);
    objectPath = path.parentPath.parentPath;
  }

  if (!callbackName) {
    return null;
  }

  if (!objectPath?.isObjectExpression()) {
    return null;
  }

  const callPath = objectPath.parentPath;
  if (!callPath?.isCallExpression()) {
    return null;
  }

  const callee = callPath.node.callee;
  if (callee?.type !== 'MemberExpression') {
    return null;
  }

  if (getKeyName(callee.object) !== 'wx') {
    return null;
  }

  const apiName = getKeyName(callee.property);
  return WX_API_PATTERNS[apiName]?.[callbackName] || null;
}

function getParamSuggestions(path) {
  const lifecycleName = getFunctionKeyName(path);
  const wechatRoot = findWeChatRoot(path);

  if (wechatRoot && lifecycleName && LIFECYCLE_PARAMS[lifecycleName]) {
    return LIFECYCLE_PARAMS[lifecycleName];
  }

  return getArraySuggestions(path) ||
    getPromiseSuggestions(path) ||
    getWxApiSuggestions(path);
}

function generateUniqueName(scope, baseName, originalName) {
  if (!baseName || baseName === originalName) {
    return originalName;
  }

  if (!scope.hasBinding(baseName) && !scope.hasGlobal(baseName)) {
    return baseName;
  }

  for (let counter = 1; counter <= 10; counter += 1) {
    const candidate = `${baseName}${counter}`;
    if (!scope.hasBinding(candidate) && !scope.hasGlobal(candidate)) {
      return candidate;
    }
  }

  return originalName;
}

function renameFunctionParams(functionPath, suggestions) {
  if (!suggestions || suggestions.length === 0) {
    return;
  }

  functionPath.node.params.forEach((param, index) => {
    if (param.type !== 'Identifier' || !isMinifiedName(param.name)) {
      return;
    }

    const suggestedName = suggestions[index];
    if (!suggestedName) {
      return;
    }

    const binding = functionPath.scope.getBinding(param.name);
    if (!binding || binding.kind !== 'param') {
      return;
    }

    const newName = generateUniqueName(functionPath.scope, suggestedName, param.name);
    if (newName !== param.name) {
      functionPath.scope.rename(param.name, newName);
    }
  });
}

function shouldRenameThisAlias(path, binding) {
  if (!binding || !binding.constant || !path.node.init || path.node.init.type !== 'ThisExpression') {
    return false;
  }

  if (!isMinifiedName(path.node.id.name)) {
    return false;
  }

  return !!findWeChatRoot(path);
}

function renameThisAlias(path) {
  if (!path.get('id').isIdentifier()) {
    return;
  }

  const originalName = path.node.id.name;
  const binding = path.scope.getBinding(originalName);
  if (!shouldRenameThisAlias(path, binding)) {
    return;
  }

  const newName = generateUniqueName(path.scope, 'page', originalName);
  if (newName === originalName) {
    return;
  }

  path.scope.rename(originalName, newName);

}

module.exports = function deobfuscateTransform() {
  return {
    name: 'deobfuscate-transform',
    visitor: {
      FunctionDeclaration(path) {
        renameFunctionParams(path, getParamSuggestions(path));
      },
      FunctionExpression(path) {
        renameFunctionParams(path, getParamSuggestions(path));
      },
      ArrowFunctionExpression(path) {
        renameFunctionParams(path, getParamSuggestions(path));
      },
      ObjectMethod(path) {
        renameFunctionParams(path, getParamSuggestions(path));
      },
      VariableDeclarator(path) {
        renameThisAlias(path);
      },
    },
  };
};
