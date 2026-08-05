import type { Metadata } from 'next';
import ObservatoryView from './observatory-view';

export const metadata: Metadata = {
  title: 'Observatory — OpenTela',
  description:
    'Measured inference throughput and time-to-first-token per GPU model on the OpenTela mesh.',
};

export default function ObservatoryPage() {
  return <ObservatoryView />;
}
