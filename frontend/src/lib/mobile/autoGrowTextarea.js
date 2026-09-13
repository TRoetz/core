/**
 * Svelte action: keep a textarea sized to its content so long titles wrap
 * instead of scrolling inside a single line (the phone editor's title field).
 * The height is recomputed on input, when the tracked value changes (Svelte
 * only passes the update hook if the caller passes it), and on window resize
 * (rotation, keyboard).
 *
 * @param {HTMLTextAreaElement} node
 * @param {unknown} [_dep] - pass the bound value so programmatic changes resize
 */
export function autoGrow(node, _dep) {
  function resize() {
    node.style.height = 'auto';
    node.style.height = `${node.scrollHeight}px`;
  }
  resize();
  node.addEventListener('input', resize);
  window.addEventListener('resize', resize);
  return {
    update() {
      resize();
    },
    destroy() {
      node.removeEventListener('input', resize);
      window.removeEventListener('resize', resize);
    },
  };
}

/**
 * Enter moves to the next field instead of inserting a newline — a title
 * field may wrap, but it stays a single logical line. Shift+Enter still
 * inserts a newline for anyone who really wants one.
 *
 * @param {HTMLTextAreaElement} node
 * @param {{ next?: HTMLElement | null }} [options]
 */
export function enterMovesFocus(node, options = {}) {
  function onKeydown(event) {
    if (event.key !== 'Enter' || event.shiftKey || event.metaKey || event.ctrlKey || event.altKey)
      return;
    event.preventDefault();
    options.next?.focus();
  }
  node.addEventListener('keydown', onKeydown);
  return {
    update(next = {}) {
      options = next;
    },
    destroy() {
      node.removeEventListener('keydown', onKeydown);
    },
  };
}
