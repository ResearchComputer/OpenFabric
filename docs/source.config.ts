import { defineDocs, defineConfig, frontmatterSchema } from 'fumadocs-mdx/config';
import githubLightDefault from 'shiki/dist/themes/github-light-default.mjs';
import lastModified from 'fumadocs-mdx/plugins/last-modified';
import { z } from 'zod';

export const docs = defineDocs({
  dir: 'content/docs',
  docs: {
    schema: frontmatterSchema.extend({
      experimental: z.boolean().optional(),
      tldr: z.string().optional(),
      date: z.string().optional(),
    }),
  },
});

// WCAG AA: the docs code background is cream (--shiki-light-bg: #faf9f5), and
// some token colors in stock light themes fall under the 4.5:1 bar there
// (classic github-light: numbers #e36209 = 3.4:1, keywords #d73a49 = 4.4:1).
// github-light-default clears AA everywhere except its comment/muted tone
// #6e7781 (4.43:1) — swap it for the theme's own darker #57606a (6.2:1).
const lightTheme = {
  ...githubLightDefault,
  tokenColors: githubLightDefault.tokenColors?.map((token) =>
    token.settings?.foreground?.toLowerCase() === '#6e7781'
      ? { ...token, settings: { ...token.settings, foreground: '#57606a' } }
      : token,
  ),
};

export default defineConfig({
  plugins: [lastModified()],
  mdxOptions: {
    rehypeCodeOptions: {
      themes: {
        light: lightTheme,
        dark: 'github-dark',
      },
    },
  },
});
