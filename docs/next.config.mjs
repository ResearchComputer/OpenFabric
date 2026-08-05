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
      { source: '/wallet', destination: '/account', permanent: true },
      { source: '/token-manager', destination: '/account', permanent: true },
      { source: '/leaderboard', destination: '/observatory', permanent: true },
    ];
  },
};

export default withMDX(config);

import('@opennextjs/cloudflare').then(m => m.initOpenNextCloudflareForDev());
