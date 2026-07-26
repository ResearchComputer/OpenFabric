import type { Metadata } from 'next';
import WalletView from './wallet-view';

export const metadata: Metadata = {
  title: 'Wallet — OpenTela Account',
};

export default function WalletPage() {
  return <WalletView />;
}
