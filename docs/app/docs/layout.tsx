import { DocsLayout } from 'fumadocs-ui/layouts/docs';
import { baseOptions } from '@/lib/layout.shared';
import { source } from '@/lib/source';
import type { ReactNode } from 'react';

export default function Layout({ children }: { children: ReactNode }) {
  return (
    <DocsLayout
      // Blog lives in the sidebar tree; don't duplicate it in the top nav.
      {...baseOptions(false)}
      tree={source.pageTree}
      sidebar={{
        // Onboarding content lives under folders (Tutorial, Advanced, …):
        // render level-1 folders expanded so /docs visitors see the full
        // navigation instead of a nearly empty sidebar.
        defaultOpenLevel: 1,
      }}
    >
      {children}
    </DocsLayout>
  );
}
