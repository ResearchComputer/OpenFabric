import { docs } from 'fumadocs-mdx:collections/server';
import { loader } from 'fumadocs-core/source';

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- fumadocs-mdx generic chain resolves to never
export const source = loader({
  baseUrl: '/docs',
  source: (docs as any).toFumadocsSource(),
});
