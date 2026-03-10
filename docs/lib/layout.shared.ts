import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';

export function baseOptions(): BaseLayoutProps {
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
        text: 'GitHub',
        url: 'https://github.com/eth-easl/opentela',
        external: true,
      },
    ],
  };
}
