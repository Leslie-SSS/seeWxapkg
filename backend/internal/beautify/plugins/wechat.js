/**
 * WeChat mini-program aware Babel plugin.
 *
 * It performs two readability-focused transforms:
 * - mark Page/Component/App/Behavior definitions with a single leading comment
 * - add short lifecycle comments for high-signal hooks
 *
 * Function-valued properties deliberately remain properties. Object methods are
 * not constructible, so changing `foo: function () {}` to `foo() {}` can alter
 * observable runtime behaviour.
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

function hasComment(node, text) {
  return !!node && ['leadingComments', 'innerComments', 'trailingComments'].some(key =>
    Array.isArray(node[key]) && node[key].some(comment => comment.value.includes(text))
  );
}

function addLeadingBlockComment(node, text, relatedNodes = []) {
  if (!node) {
    return;
  }

  const exists = [node, ...relatedNodes].some(candidate => hasComment(candidate, text));

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

module.exports = function wechatTransform() {
  return {
    name: 'wechat-patterns',
    visitor: {
      CallExpression(path) {
        const rootName = getKeyName(path.node.callee);
        if (!ROOT_TYPES.has(rootName)) {
          return;
        }

        addLeadingBlockComment(path.node, `${rootName} Definition`, [path.parentPath?.node]);
      },

      ObjectProperty(path) {
        if (!path.get('value').isFunctionExpression()) {
          return;
        }

        if (!getWeChatRootName(path)) {
          return;
        }

        const methodName = getKeyName(path.node.key);
        const comment = LIFECYCLE_COMMENTS[methodName];
        if (comment) {
          addLeadingBlockComment(path.node, comment);
        }
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
