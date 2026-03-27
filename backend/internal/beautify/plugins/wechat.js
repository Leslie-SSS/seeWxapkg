/**
 * WeChat mini-program aware Babel plugin.
 *
 * It performs two readability-focused transforms:
 * - mark Page/Component/App/Behavior definitions with a single leading comment
 * - normalize `foo: function() {}` into object-method syntax inside WeChat objects
 * - add short lifecycle comments for high-signal hooks
 */

const ROOT_TYPES = new Set(['App', 'Behavior', 'Component', 'Page']);

const LIFECYCLE_COMMENTS = {
  attached: 'Component attached to page',
  created: 'Component created',
  detached: 'Component detached from page',
  onHide: 'Page hidden',
  onLaunch: 'App launched',
  onLoad: 'Page loaded - receive options from navigation',
  onPullDownRefresh: 'Pull down refresh triggered',
  onReachBottom: 'Reached bottom of page',
  onReady: 'Page ready for interaction',
  onShareAppMessage: 'Share to friends/groups',
  onShow: 'Page shown',
  onUnload: 'Page unloaded',
  ready: 'Component ready',
};

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

function addLeadingBlockComment(node, text) {
  if (!node) {
    return;
  }

  const exists = Array.isArray(node.leadingComments) &&
    node.leadingComments.some(comment => comment.value.includes(text));

  if (!exists) {
    node.leadingComments = node.leadingComments || [];
    node.leadingComments.unshift({
      type: 'CommentBlock',
      value: ` ${text} `,
    });
  }
}

function getWeChatRootName(path) {
  let current = path;

  while (current) {
    if (current.isCallExpression()) {
      const calleeName = getKeyName(current.node.callee);
      if (ROOT_TYPES.has(calleeName)) {
        return calleeName;
      }
    }

    current = current.parentPath;
  }

  return null;
}

function toObjectMethod(t, propertyPath) {
  const property = propertyPath.node;
  if (
    !propertyPath.isObjectProperty() ||
    property.computed ||
    property.shorthand ||
    property.value?.type !== 'FunctionExpression'
  ) {
    return;
  }

  const method = t.objectMethod(
    'method',
    property.key,
    property.value.params,
    property.value.body,
    property.computed,
  );
  method.async = property.value.async;
  method.generator = property.value.generator;
  method.returnType = property.value.returnType;
  method.typeParameters = property.value.typeParameters;
  method.leadingComments = property.leadingComments || property.value.leadingComments || [];
  method.trailingComments = property.trailingComments || property.value.trailingComments || [];
  method.loc = property.loc;

  propertyPath.replaceWith(method);
}

module.exports = function wechatTransform({ types: t }) {
  return {
    name: 'wechat-patterns',
    visitor: {
      CallExpression(path) {
        const rootName = getKeyName(path.node.callee);
        if (!ROOT_TYPES.has(rootName)) {
          return;
        }

        addLeadingBlockComment(path.node, `${rootName} Definition`);
      },

      ObjectProperty(path) {
        if (!path.get('value').isFunctionExpression()) {
          return;
        }

        if (!getWeChatRootName(path)) {
          return;
        }

        toObjectMethod(t, path);
      },

      ObjectMethod(path) {
        if (!getWeChatRootName(path)) {
          return;
        }

        const methodName = getKeyName(path.node.key);
        const comment = LIFECYCLE_COMMENTS[methodName];
        if (comment) {
          addLeadingBlockComment(path.node, comment);
        }
      },
    },
  };
};

module.exports.LIFECYCLE_COMMENTS = LIFECYCLE_COMMENTS;
module.exports.ROOT_TYPES = ROOT_TYPES;
