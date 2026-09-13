// Milkdown serializes hard breaks in the middle of a paragraph as literal
// `<br />` HTML, but its hardbreak schema only parses remark `break` nodes —
// the HTML spellings come back as visible text (or get dropped) in the visual
// editor and then round-trip forever. This remark plugin rewrites
// `<br>`-style html nodes into real break nodes before Milkdown maps the
// tree, so the editor shows a line break and the next save normalizes the
// content to Markdown hard breaks.

const BREAK_HTML = /^<br\s*\/?>$/i;

// Parents whose children are inline: a bare break node is valid there.
const INLINE_PARENTS = new Set(['paragraph']);

// Block containers where a stray break must be wrapped in a paragraph to stay
// valid mdast (a break is an inline node).
const BLOCK_PARENTS = new Set(['root', 'blockquote', 'listItem']);

function isBreakHTML(node) {
  return (
    node?.type === 'html' && typeof node.value === 'string' && BREAK_HTML.test(node.value.trim())
  );
}

function replacementFor(parent) {
  if (INLINE_PARENTS.has(parent.type)) return { type: 'break' };
  if (BLOCK_PARENTS.has(parent.type)) return { type: 'paragraph', children: [{ type: 'break' }] };
  return null;
}

function transform(node) {
  if (!node || !Array.isArray(node.children)) return;
  const children = node.children;
  for (let i = 0; i < children.length; i++) {
    const child = children[i];
    if (isBreakHTML(child)) {
      const replacement = replacementFor(node);
      if (replacement) children[i] = replacement;
      continue;
    }
    transform(child);
  }
}

/**
 * Unified plugin (attacher): convert `<br>`, `<br/>`, and `<br />` html nodes
 * (inline and block level) into hard break nodes. Wired into the editor via
 * `ctx.update(remarkCtx, (processor) => processor.use(rewriteBreakHTML))` —
 * config-time updates are deterministic, unlike `$remark` whose plugin races
 * the schema step that snapshots the remark processor. Exported separately so
 * tests can run it through plain unified, without the editor context.
 */
export const rewriteBreakHTML = () => (tree) => transform(tree);
