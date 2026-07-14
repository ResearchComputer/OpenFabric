import type { Metadata } from 'next';
import TokenManagerClient from './token-manager-client';

export const metadata: Metadata = {
  title: 'OpenTela Wallet',
  description:
    'Connect a Solana wallet, manage OpenTela API keys, view OTELA balances, and transfer OTELA.',
};

export default function TokenManagerPage() {
  return <TokenManagerClient />;
}
