import './global.css';
import { RootProvider } from 'fumadocs-ui/provider/next';
import { Inter, JetBrains_Mono, Newsreader } from 'next/font/google';
import type { ReactNode } from 'react';
import { SkipLink } from './skip-link';

const inter = Inter({
  subsets: ['latin'],
  variable: '--font-inter',
  display: 'swap',
});

// Serif display face for prose and page headings — docs.ada.ai uses the same.
const newsreader = Newsreader({
  subsets: ['latin'],
  variable: '--font-newsreader',
  display: 'swap',
});

const jetbrainsMono = JetBrains_Mono({
  subsets: ['latin'],
  variable: '--font-jetbrains-mono',
  display: 'swap',
});

export const metadata = {
  title: {
    default: 'OpenTela Documentation',
    template: '%s — OpenTela',
  },
  description:
    'Documentation for OpenTela — a decentralized distributed computing platform for GPU orchestration.',
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html
      lang="en"
      className={`${inter.variable} ${newsreader.variable} ${jetbrainsMono.variable}`}
      suppressHydrationWarning
    >
      {/* suppressHydrationWarning: browser extensions (wallet, Grammarly, etc.)
          inject attributes on <body> before React hydrates; this silences that
          one-level attribute diff only, not mismatches in our own tree. */}
      <body suppressHydrationWarning>
        <SkipLink />
        <RootProvider>{children}</RootProvider>
      </body>
    </html>
  );
}
