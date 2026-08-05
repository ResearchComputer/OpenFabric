// Neutralizes the dead `@vercel/og` import that OpenNext unconditionally emits
// into the Cloudflare server function, even when ImageResponse is never used.
//
// OpenNext rewrites `next/dist/compiled/@vercel/og/index.node.js` to a literal
// `import("next/dist/compiled/@vercel/og/index.edge.js")`. Wrangler's bundler
// then statically resolves that literal and pulls in `index.edge.js` together
// with `resvg.wasm` (~1.3 MB) and `yoga.wasm` (~87 KB) — about 700 KiB gzipped
// of dead weight that pushes the Worker past the free-tier 3 MiB limit.
//
// This script runs after `opennextjs-cloudflare build` and replaces that
// dynamic import with `Promise.resolve({})`. Since no route in this app uses
// `ImageResponse`, the branch is never hit at runtime. If a route ever starts
// using `next/og`, remove this script (or switch to a paid Workers plan).
import { readFileSync, writeFileSync, statSync } from "node:fs";

const HANDLER = ".open-next/server-functions/default/handler.mjs";

const NEEDLE = 'await import("next/dist/compiled/@vercel/og/index.edge.js")';
const REPLACEMENT = "Promise.resolve({})";

const before = readFileSync(HANDLER, "utf8");
const count = before.split(NEEDLE).length - 1;

if (count === 0) {
  console.error(
    "strip-vercel-og: target import not found in handler.mjs.\n" +
      "OpenNext may have changed; verify and update this script.",
  );
  process.exit(1);
}

const after = before.split(NEEDLE).join(REPLACEMENT);
writeFileSync(HANDLER, after);

const fmt = (b) => `${(b / 1024).toFixed(0)} KiB`;
console.log(
  `strip-vercel-og: replaced ${count} occurrence(s) of the @vercel/og import ` +
    `(${fmt(before.length)} -> ${fmt(after.length)} handler.mjs).`,
);
