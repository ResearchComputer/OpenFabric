import { source } from '@/lib/source';
import {
  DocsPage,
  DocsBody,
  DocsDescription,
  DocsTitle,
} from 'fumadocs-ui/page';
import { notFound } from 'next/navigation';
import defaultMdxComponents from 'fumadocs-ui/mdx';
import type { MDXContent } from 'mdx/types';

export default async function Page(props: {
  params: Promise<{ slug?: string[] }>;
}) {
  const params = await props.params;
  const page = source.getPage(params.slug);
  if (!page) notFound();

  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- fumadocs-mdx generic chain loses type info
  const data = page.data as any;
  const MDX = data.body as MDXContent;

  return (
    <DocsPage
      toc={data.toc}
      full={data.full}
      editOnGithub={{
        owner: 'eth-easl',
        repo: 'OpenTela',
        sha: 'main',
        // repo root is one level up from the docs app, then the content dir
        path: `docs/content/docs/${page.path}`,
      }}
      lastUpdate={
        data.lastModified ? new Date(data.lastModified as string) : undefined
      }
    >
      <DocsTitle>{page.data.title}</DocsTitle>
      {data.experimental && (
        <div className="mt-2 mb-1">
          <span
            role="status"
            className="inline-flex items-center gap-1.5 rounded-full border border-amber-500/40 bg-amber-500/10 px-2.5 py-0.5 text-xs font-medium text-amber-700 dark:text-amber-300"
          >
            <svg
              aria-hidden="true"
              viewBox="0 0 24 24"
              className="size-3.5"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="M12 9v4" />
              <path d="M12 17h.01" />
              <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0Z" />
            </svg>
            Experimental
          </span>
        </div>
      )}
      <DocsDescription>{page.data.description}</DocsDescription>
      <DocsBody>
        <MDX components={{ ...defaultMdxComponents }} />
      </DocsBody>
    </DocsPage>
  );
}

export async function generateStaticParams() {
  return source.generateParams();
}

export async function generateMetadata(props: {
  params: Promise<{ slug?: string[] }>;
}) {
  const params = await props.params;
  const page = source.getPage(params.slug);
  if (!page) notFound();

  return {
    title: page.data.title,
    description: page.data.description,
  };
}
