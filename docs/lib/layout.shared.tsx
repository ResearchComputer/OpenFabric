import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';

// Rendered with aria-hidden: the wrapping icon link carries `label` as its
// aria-label, so a role/alt on the SVG itself is redundant and trips
// accessibility scanners (axe svg-img-alt).
const GitHubIcon = (
  <svg aria-hidden="true" viewBox="0 0 24 24" fill="currentColor">
    <path d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.084-.729.084-.729 1.205.084 1.84 1.236 1.84 1.236 1.07 1.834 2.809 1.304 3.495.997.108-.776.418-1.305.762-1.605-2.665-.301-5.466-1.332-5.466-5.93 0-1.31.469-2.381 1.236-3.221-.124-.303-.535-1.523.117-3.176 0 0 1.008-.322 3.301 1.23a11.52 11.52 0 0 1 3.003-.404c1.018.005 2.045.138 3.003.404 2.291-1.553 3.297-1.23 3.297-1.23.653 1.653.242 2.873.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222 0 1.606-.014 2.898-.014 3.293 0 .322.216.694.825.576C20.565 21.796 24 17.299 24 12c0-6.627-5.373-12-12-12" />
  </svg>
);

/**
 * @param showBlogLink - The docs layout renders a Blog section in its
 * sidebar, so it passes `false` to avoid a duplicate entry in the top nav.
 */
export function baseOptions(showBlogLink = true): BaseLayoutProps {
  return {
    nav: {
      title: 'OpenTela',
      url: '/',
    },
    links: [
      {
        text: 'Docs',
        url: '/docs',
      },
      {
        text: 'Observatory',
        url: '/observatory',
      },
      {
        text: 'Account',
        url: '/account',
      },
      ...(showBlogLink
        ? [
            {
              text: 'Blog',
              url: '/docs/blog',
            },
          ]
        : []),
      {
        type: 'icon',
        label: 'GitHub',
        icon: GitHubIcon,
        text: 'GitHub',
        url: 'https://github.com/eth-easl/OpenTela',
        external: true,
      },
    ],
  };
}
