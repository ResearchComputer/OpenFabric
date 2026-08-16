'use client';

import type { MouseEvent } from 'react';

/**
 * Skip-to-content link rendered as the first focusable element on every page.
 *
 * The main-content id differs per surface (fumadocs renders docs articles as
 * `#nd-page`; the marketing/observatory/account shells use `#nd-main`), so the
 * click handler resolves the target at runtime instead of relying on the hash
 * alone. The href is kept for the docs case so the link still works without JS.
 */
export function SkipLink() {
  function onClick(e: MouseEvent<HTMLAnchorElement>) {
    const target =
      document.getElementById('nd-page') ??
      document.getElementById('nd-main') ??
      document.querySelector('main');
    if (!target) return;
    e.preventDefault();
    if (!target.hasAttribute('tabindex')) {
      target.setAttribute('tabindex', '-1');
    }
    target.focus({ preventScroll: true });
    target.scrollIntoView();
  }

  return (
    <a href="#nd-page" className="skip-link" onClick={onClick}>
      Skip to content
    </a>
  );
}
