import Link from 'next/link';

const features = [
  {
    title: 'Peer-to-Peer Orchestration',
    description:
      'Decentralized resource scheduling over a libp2p mesh network. No single point of failure.',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" className="size-6">
        <path strokeLinecap="round" strokeLinejoin="round" d="M7.5 21 3 16.5m0 0L7.5 12M3 16.5h13.5m0-13.5L21 7.5m0 0L16.5 12M21 7.5H7.5" />
      </svg>
    ),
  },
  {
    title: 'GPU-Native',
    description:
      'Built for GPU workloads. Auto-detects hardware via nvidia-smi and integrates with Slurm clusters.',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" className="size-6">
        <path strokeLinecap="round" strokeLinejoin="round" d="M8.25 3v1.5M4.5 8.25H3m18 0h-1.5M4.5 12H3m18 0h-1.5m-15 3.75H3m18 0h-1.5M8.25 19.5V21M12 3v1.5m0 15V21m3.75-18v1.5m0 15V21m-9-1.5h10.5a2.25 2.25 0 0 0 2.25-2.25V6.75a2.25 2.25 0 0 0-2.25-2.25H6.75A2.25 2.25 0 0 0 4.5 6.75v10.5a2.25 2.25 0 0 0 2.25 2.25Z" />
      </svg>
    ),
  },
  {
    title: 'CRDT Consensus',
    description:
      'Conflict-free replicated state with no central coordinator. Gossip-based propagation across all peers.',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" className="size-6">
        <path strokeLinecap="round" strokeLinejoin="round" d="M20.25 6.375c0 2.278-3.694 4.125-8.25 4.125S3.75 8.653 3.75 6.375m16.5 0c0-2.278-3.694-4.125-8.25-4.125S3.75 4.097 3.75 6.375m16.5 0v11.25c0 2.278-3.694 4.125-8.25 4.125s-8.25-1.847-8.25-4.125V6.375m16.5 0v3.75m-16.5-3.75v3.75m16.5 0v3.75C20.25 16.153 16.556 18 12 18s-8.25-1.847-8.25-4.125v-3.75m16.5 0c0 2.278-3.694 4.125-8.25 4.125s-8.25-1.847-8.25-4.125" />
      </svg>
    ),
  },
  {
    title: 'Smart Routing',
    description:
      'Three-tier request matching with automatic fallback. Route by model, capability, or catch-all.',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" className="size-6">
        <path strokeLinecap="round" strokeLinejoin="round" d="M7.5 21 3 16.5m0 0L7.5 12M3 16.5h13.5m0-13.5L21 7.5m0 0L16.5 12M21 7.5H7.5" />
        <path strokeLinecap="round" strokeLinejoin="round" d="M3.75 12h16.5" />
      </svg>
    ),
  },
  {
    title: 'OpenAI-Compatible API',
    description:
      'Drop-in replacement for OpenAI endpoints. Point your existing apps at an OpenTela cluster.',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" className="size-6">
        <path strokeLinecap="round" strokeLinejoin="round" d="M17.25 6.75 22.5 12l-5.25 5.25m-10.5 0L1.5 12l5.25-5.25m7.5-3-4.5 16.5" />
      </svg>
    ),
  },
];

function MeshBackground() {
  return (
    <svg
      className="pointer-events-none absolute inset-0 h-full w-full"
      viewBox="0 0 800 400"
      fill="none"
      preserveAspectRatio="xMidYMid slice"
    >
      {/* Nodes */}
      <circle cx="80" cy="80" r="3" className="fill-fd-primary/20" />
      <circle cx="200" cy="50" r="4" className="fill-fd-primary/25" />
      <circle cx="350" cy="90" r="3" className="fill-fd-primary/20" />
      <circle cx="500" cy="60" r="4" className="fill-fd-primary/25" />
      <circle cx="650" cy="85" r="3" className="fill-fd-primary/20" />
      <circle cx="720" cy="45" r="3" className="fill-fd-primary/15" />
      <circle cx="130" cy="180" r="3" className="fill-fd-primary/20" />
      <circle cx="300" cy="200" r="4" className="fill-fd-primary/25" />
      <circle cx="450" cy="170" r="3" className="fill-fd-primary/20" />
      <circle cx="600" cy="210" r="3" className="fill-fd-primary/20" />
      <circle cx="750" cy="180" r="4" className="fill-fd-primary/15" />
      <circle cx="50" cy="300" r="3" className="fill-fd-primary/15" />
      <circle cx="180" cy="320" r="3" className="fill-fd-primary/20" />
      <circle cx="400" cy="310" r="4" className="fill-fd-primary/20" />
      <circle cx="550" cy="330" r="3" className="fill-fd-primary/15" />
      <circle cx="700" cy="300" r="3" className="fill-fd-primary/15" />
      {/* Edges */}
      <line x1="80" y1="80" x2="200" y2="50" className="stroke-fd-primary/10" strokeWidth="0.5" />
      <line x1="200" y1="50" x2="350" y2="90" className="stroke-fd-primary/10" strokeWidth="0.5" />
      <line x1="350" y1="90" x2="500" y2="60" className="stroke-fd-primary/10" strokeWidth="0.5" />
      <line x1="500" y1="60" x2="650" y2="85" className="stroke-fd-primary/10" strokeWidth="0.5" />
      <line x1="650" y1="85" x2="720" y2="45" className="stroke-fd-primary/10" strokeWidth="0.5" />
      <line x1="80" y1="80" x2="130" y2="180" className="stroke-fd-primary/10" strokeWidth="0.5" />
      <line x1="200" y1="50" x2="300" y2="200" className="stroke-fd-primary/8" strokeWidth="0.5" />
      <line x1="350" y1="90" x2="450" y2="170" className="stroke-fd-primary/10" strokeWidth="0.5" />
      <line x1="500" y1="60" x2="600" y2="210" className="stroke-fd-primary/8" strokeWidth="0.5" />
      <line x1="650" y1="85" x2="750" y2="180" className="stroke-fd-primary/10" strokeWidth="0.5" />
      <line x1="130" y1="180" x2="300" y2="200" className="stroke-fd-primary/10" strokeWidth="0.5" />
      <line x1="300" y1="200" x2="450" y2="170" className="stroke-fd-primary/10" strokeWidth="0.5" />
      <line x1="450" y1="170" x2="600" y2="210" className="stroke-fd-primary/10" strokeWidth="0.5" />
      <line x1="600" y1="210" x2="750" y2="180" className="stroke-fd-primary/10" strokeWidth="0.5" />
      <line x1="50" y1="300" x2="180" y2="320" className="stroke-fd-primary/8" strokeWidth="0.5" />
      <line x1="180" y1="320" x2="400" y2="310" className="stroke-fd-primary/8" strokeWidth="0.5" />
      <line x1="400" y1="310" x2="550" y2="330" className="stroke-fd-primary/8" strokeWidth="0.5" />
      <line x1="550" y1="330" x2="700" y2="300" className="stroke-fd-primary/8" strokeWidth="0.5" />
      <line x1="130" y1="180" x2="180" y2="320" className="stroke-fd-primary/8" strokeWidth="0.5" />
      <line x1="300" y1="200" x2="400" y2="310" className="stroke-fd-primary/8" strokeWidth="0.5" />
      <line x1="450" y1="170" x2="550" y2="330" className="stroke-fd-primary/8" strokeWidth="0.5" />
      <line x1="600" y1="210" x2="700" y2="300" className="stroke-fd-primary/8" strokeWidth="0.5" />
    </svg>
  );
}

export default function HomePage() {
  return (
    <main className="flex min-h-dvh flex-col">
      {/* Hero */}
      <section className="relative flex flex-col items-center justify-center overflow-hidden px-6 pt-24 pb-16 text-center">
        {/* Gradient blob */}
        <div className="pointer-events-none absolute -top-32 h-[500px] w-[500px] rounded-full bg-fd-primary/10 blur-[120px]" />

        {/* Mesh */}
        <MeshBackground />

        <div className="relative">
          <p className="mb-4 text-sm font-medium tracking-widest uppercase text-fd-primary">
            Decentralized GPU Orchestration
          </p>
          <h1 className="mx-auto max-w-3xl text-5xl font-extrabold leading-tight tracking-tight sm:text-6xl">
            Distribute compute{' '}
            <span className="bg-gradient-to-r from-fd-primary to-fd-primary/60 bg-clip-text text-transparent">
              across the mesh
            </span>
          </h1>
          <p className="mx-auto mt-6 max-w-2xl text-lg text-fd-muted-foreground leading-relaxed">
            OpenTela is a peer-to-peer platform for orchestrating distributed GPU
            resources.
          </p>
          <div className="mt-10 flex flex-wrap items-center justify-center gap-4">
            <Link
              href="/docs"
              className="rounded-lg bg-fd-primary px-6 py-3 text-sm font-medium text-fd-primary-foreground shadow-lg shadow-fd-primary/20 hover:bg-fd-primary/90 transition-colors"
            >
              Get Started
            </Link>
            <a
              href="https://github.com/eth-easl/opentela"
              target="_blank"
              rel="noopener noreferrer"
              className="rounded-lg border border-fd-border bg-fd-background px-6 py-3 text-sm font-medium text-fd-foreground hover:bg-fd-muted transition-colors"
            >
              GitHub
            </a>
          </div>
        </div>

        {/* Terminal preview */}
        <div className="relative mt-14 w-full max-w-xl">
          <div className="overflow-hidden rounded-xl border border-fd-border bg-fd-card shadow-xl shadow-fd-primary/5">
            <div className="flex items-center gap-2 border-b border-fd-border px-4 py-3">
              <div className="size-3 rounded-full bg-fd-muted-foreground/20" />
              <div className="size-3 rounded-full bg-fd-muted-foreground/20" />
              <div className="size-3 rounded-full bg-fd-muted-foreground/20" />
              <span className="ml-2 text-xs text-fd-muted-foreground">terminal</span>
            </div>
            <pre className="overflow-x-auto p-4 text-[13px] leading-relaxed">
              {/* <code>
                <span className="text-fd-primary">$</span>
                <span className="text-fd-foreground"> curl -fsSL https://get.opentela.dev | sh</span>
                {'\n'}
                <span className="text-fd-primary">$</span>
                <span className="text-fd-foreground"> opentela start --role head</span>
                {'\n'}
                <span className="text-fd-muted-foreground">{'  '}Listening on :8080</span>
                {'\n'}
                <span className="text-fd-muted-foreground">{'  '}Discovered 12 peers via gossip</span>
                {'\n'}
                <span className="text-green-500">{'  '}✓ Mesh ready</span>
              </code> */}
            </pre>
          </div>
        </div>
      </section>

      {/* Features
      <section className="mx-auto w-full max-w-6xl px-6 py-20">
        <h2 className="mb-2 text-center text-sm font-medium tracking-widest uppercase text-fd-muted-foreground">
          Features
        </h2>
        <p className="mx-auto mb-12 max-w-xl text-center text-2xl font-bold tracking-tight">
          Everything you need for decentralized inference
        </p>
        <div className="grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
          {features.map((feature) => (
            <div
              key={feature.title}
              className="group rounded-xl border border-fd-border bg-fd-card p-6 transition-colors hover:border-fd-primary/40 hover:bg-fd-card/80"
            >
              <div className="mb-4 flex size-10 items-center justify-center rounded-lg bg-fd-primary/10 text-fd-primary">
                {feature.icon}
              </div>
              <h3 className="mb-2 font-semibold">{feature.title}</h3>
              <p className="text-sm leading-relaxed text-fd-muted-foreground">
                {feature.description}
              </p>
            </div>
          ))}
        </div>
      </section> */}

      {/* Footer */}
      <footer className="mt-auto border-t border-fd-border py-8">
        <div className="mx-auto flex max-w-6xl flex-col items-center justify-between gap-4 px-6 sm:flex-row">
          <p className="text-sm text-fd-muted-foreground">
            OpenTela &mdash; Open-source decentralized computing
          </p>
          <div className="flex gap-6">
            <Link href="/docs" className="text-sm text-fd-muted-foreground hover:text-fd-foreground transition-colors">
              Documentation
            </Link>
            <a
              href="https://github.com/eth-easl/opentela"
              target="_blank"
              rel="noopener noreferrer"
              className="text-sm text-fd-muted-foreground hover:text-fd-foreground transition-colors"
            >
              GitHub
            </a>
          </div>
        </div>
      </footer>
    </main>
  );
}
