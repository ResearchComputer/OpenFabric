import { HomeLayout } from 'fumadocs-ui/layouts/home';
import { baseOptions } from '@/lib/layout.shared';
import type { ReactNode } from 'react';

export default function ObservatoryLayout({ children }: { children: ReactNode }) {
  return <HomeLayout {...baseOptions()}>{children}</HomeLayout>;
}
