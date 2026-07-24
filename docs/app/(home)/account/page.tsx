import type { Metadata } from 'next';
import AccountClient from './account-client';

export const metadata: Metadata = {
  title: 'OpenTela Account',
  description:
    'Sign in with a Solana wallet or an OpenTela account, manage API keys, view OTELA balances, and browse running services.',
};

export default function AccountPage() {
  return <AccountClient />;
}
