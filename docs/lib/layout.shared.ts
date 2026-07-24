import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';

export function baseOptions(): BaseLayoutProps {
  return {
    nav: {
      title: 'OpenTela',
      url: '/',
    },
    githubUrl: 'https://github.com/eth-easl/OpenTela',
    links: [
      {
        text: 'Docs',
        url: '/docs',
      },
      {
        text: 'Account',
        url: '/account',
      },
      {
        text: 'Blog',
        url: '/docs/blog',
      },
    ],
  };
}
