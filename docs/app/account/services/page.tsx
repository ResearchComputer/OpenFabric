import type { Metadata } from 'next';
import ServicesView from './services-view';

export const metadata: Metadata = {
  title: 'Services — OpenTela Account',
};

export default function ServicesPage() {
  return <ServicesView />;
}
