import './global.css';
import { RootProvider } from 'fumadocs-ui/provider/next';
import { Inter } from 'next/font/google';
import type { ReactNode } from 'react';

const inter = Inter({
  subsets: ['latin'],
  variable: '--font-inter',
  display: 'swap',
});

export const metadata = {
  title: 'OpenTela Documentation',
  description:
    'Documentation for OpenTela — a decentralized distributed computing platform for GPU orchestration.',
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en" className={inter.variable} suppressHydrationWarning>
      {/* suppressHydrationWarning: browser extensions (wallet, Grammarly, etc.)
          inject attributes on <body> before React hydrates; this silences that
          one-level attribute diff only, not mismatches in our own tree. */}
      <body suppressHydrationWarning>
        <RootProvider>{children}</RootProvider>
      </body>
    </html>
  );
}
