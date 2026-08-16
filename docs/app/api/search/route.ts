import { source } from '@/lib/source';
import {
  createSearchAPI,
  type AdvancedIndex,
} from 'fumadocs-core/search/server';

// Proposals describe unimplemented designs and are not user-facing. They are
// excluded from the sidebar (via `!proposals` in meta.json) and from search
// here, but remain reachable by direct URL at /docs/proposals/*.
const isProposal = (url: string) => url.startsWith('/docs/proposals');

export const { GET } = createSearchAPI('advanced', {
  async indexes() {
    const pages = source.getPages().filter((page) => !isProposal(page.url));

    return Promise.all(
      pages.map(async (page): Promise<AdvancedIndex> => {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any -- fumadocs-mdx generic chain loses type info
        const data = page.data as any;
        // Mirrors fumadocs' default index builder: MDX pages may expose
        // structuredData as a value or a lazy function.
        const structuredData =
          typeof data.structuredData === 'function'
            ? await data.structuredData()
            : data.structuredData;

        return {
          id: page.url,
          title: data.title,
          description: data.description,
          url: page.url,
          structuredData,
        };
      }),
    );
  },
});
