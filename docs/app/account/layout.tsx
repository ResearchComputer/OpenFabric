import type { Metadata } from 'next';
import type { ReactNode } from 'react';
import AccountShell from './account-shell';

export const metadata: Metadata = {
  title: 'OpenTela Account',
  description:
    'Manage OpenTela API keys, browse running services, and connect a Solana wallet.',
};

export default function AccountLayout({ children }: { children: ReactNode }) {
  return <AccountShell>{children}</AccountShell>;
}
