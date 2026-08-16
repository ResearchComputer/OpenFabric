import { HomeLayout } from 'fumadocs-ui/layouts/home';
import { baseOptions } from '@/lib/layout.shared';
import Link from 'next/link';

export default function NotFound() {
  return (
    <HomeLayout {...baseOptions()}>
      <main id="nd-main" className="flex flex-1 flex-col">
        <section className="relative flex flex-1 flex-col items-center justify-center overflow-hidden px-6 py-24 text-center">
          {/* Match the home hero decoration so the page feels part of the site */}
          <div className="grid-bg pointer-events-none absolute inset-0" />
          <div className="pointer-events-none absolute -top-32 left-1/2 h-[400px] w-[400px] -translate-x-1/2 rounded-full bg-fd-primary/10 blur-[120px]" />

          <div className="relative max-w-lg">
            <p className="text-sm font-medium uppercase tracking-widest text-fd-primary">
              404
            </p>
            <h1 className="font-display mt-2 text-4xl sm:text-5xl">
              Page not found
            </h1>
            <p className="mt-4 text-fd-muted-foreground">
              The page you are looking for doesn&rsquo;t exist or may have been
              moved. Try the documentation home, or press{' '}
              <kbd className="rounded border border-fd-border bg-fd-muted px-1.5 py-0.5 text-xs font-medium">
                ⌘K
              </kbd>{' '}
              to search the site.
            </p>
            <div className="mt-8 flex flex-wrap items-center justify-center gap-3">
              <Link
                href="/docs"
                className="rounded-lg bg-fd-primary px-5 py-2.5 text-sm font-medium text-fd-primary-foreground shadow-lg shadow-fd-primary/20 transition-colors hover:bg-fd-primary/90"
              >
                Go to documentation
              </Link>
              <Link
                href="/"
                className="rounded-lg border border-fd-border bg-fd-background px-5 py-2.5 text-sm font-medium text-fd-foreground transition-colors hover:bg-fd-muted"
              >
                Back to home
              </Link>
            </div>
          </div>
        </section>
      </main>
    </HomeLayout>
  );
}
