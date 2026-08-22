import { createMDX } from 'fumadocs-mdx/next';

const withMDX = createMDX();

/** @type {import('next').NextConfig} */
const config = {
  reactStrictMode: true,
  images: {
    remotePatterns: [
      {
        hostname: 'www.cscs.ch',
      },
    ],
  },
  async redirects() {
    return [
      {
        source: '/account/:path*',
        destination: 'https://cloud.opentela.ai/account/:path*',
        permanent: true,
      },
      {
        source: '/auth/:path*',
        destination: 'https://cloud.opentela.ai/auth/:path*',
        permanent: true,
      },
      {
        source: '/wallet',
        destination: 'https://cloud.opentela.ai/account/wallet',
        permanent: true,
      },
      {
        source: '/token-manager',
        destination: 'https://cloud.opentela.ai/account',
        permanent: true,
      },
      { source: '/leaderboard', destination: '/observatory', permanent: true },
    ];
  },
};

export default withMDX(config);

import('@opennextjs/cloudflare').then(m => m.initOpenNextCloudflareForDev());
