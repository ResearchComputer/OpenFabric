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
    title: 'On-Chain Settlement',
    description:
      'Dual-attestation billing with Solana-based settlement for transparent, verifiable resource accounting.',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" className="size-6">
        <path strokeLinecap="round" strokeLinejoin="round" d="M2.25 18.75a60.07 60.07 0 0 1 15.797 2.101c.727.198 1.453-.342 1.453-1.096V18.75M3.75 4.5v.75A.75.75 0 0 1 3 6h-.75m0 0v-.375c0-.621.504-1.125 1.125-1.125H20.25M2.25 6v9m18-10.5v.75c0 .414.336.75.75.75h.75m-1.5-1.5h.375c.621 0 1.125.504 1.125 1.125v9.75c0 .621-.504 1.125-1.125 1.125h-.375m1.5-1.5H21a.75.75 0 0 0-.75.75v.75m0 0H3.75m0 0h-.375a1.125 1.125 0 0 1-1.125-1.125V15m1.5 1.5v-.75A.75.75 0 0 0 3 15h-.75M15 10.5a3 3 0 1 1-6 0 3 3 0 0 1 6 0Zm3 0h.008v.008H18V10.5Zm-12 0h.008v.008H6V10.5Z" />
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

export default function HomePage() {
  return (
    <main className="flex min-h-screen flex-col">
      {/* Nav */}
      <header className="sticky top-0 z-50 border-b border-fd-border bg-fd-background/80 backdrop-blur-sm">
        <div className="mx-auto flex h-14 max-w-6xl items-center justify-between px-6">
          <span className="text-lg font-semibold tracking-tight">OpenTela</span>
          <nav className="flex items-center gap-4">
            <Link href="/docs" className="text-sm text-fd-muted-foreground hover:text-fd-foreground transition-colors">
              Docs
            </Link>
            <a
              href="https://github.com/opentela/opentela"
              target="_blank"
              rel="noopener noreferrer"
              className="text-sm text-fd-muted-foreground hover:text-fd-foreground transition-colors"
            >
              GitHub
            </a>
          </nav>
        </div>
      </header>

      {/* Hero */}
      <section className="relative flex flex-col items-center justify-center px-6 pt-24 pb-16 text-center">
        {/* Gradient blob */}
        <div className="pointer-events-none absolute -top-32 h-[500px] w-[500px] rounded-full bg-fd-primary/10 blur-[120px]" />

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
            resources. No central coordinator — just nodes, gossip, and CRDTs.
          </p>
          <div className="mt-10 flex flex-wrap items-center justify-center gap-4">
            <Link
              href="/docs"
              className="rounded-lg bg-fd-primary px-6 py-3 text-sm font-medium text-fd-primary-foreground shadow-lg shadow-fd-primary/20 hover:bg-fd-primary/90 transition-colors"
            >
              Get Started
            </Link>
            <Link
              href="/docs/tutorial/installation"
              className="rounded-lg border border-fd-border bg-fd-background px-6 py-3 text-sm font-medium text-fd-foreground hover:bg-fd-muted transition-colors"
            >
              Installation Guide
            </Link>
          </div>
        </div>
      </section>

      {/* Features */}
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
      </section>

      {/* Quick start snippet */}
      <section className="mx-auto w-full max-w-6xl px-6 py-16">
        <div className="rounded-2xl border border-fd-border bg-fd-card p-8 sm:p-12">
          <div className="flex flex-col gap-8 lg:flex-row lg:items-center lg:justify-between">
            <div className="max-w-md">
              <h2 className="text-2xl font-bold tracking-tight">Up and running in minutes</h2>
              <p className="mt-3 text-fd-muted-foreground leading-relaxed">
                Install the binary, point to your GPU service, and join the mesh.
                OpenTela handles discovery, routing, and load balancing.
              </p>
            </div>
            <div className="flex-1 lg:max-w-md">
              <pre className="overflow-x-auto rounded-lg border border-fd-border bg-fd-background p-4 text-sm leading-relaxed">
                <code>{`# Install
curl -fsSL https://get.opentela.dev | sh

# Start a worker node
opentela start --role worker \\
  --service-port 8000 \\
  --identity model=Qwen/Qwen3-8B

# Start the head node
opentela start --role head`}</code>
              </pre>
            </div>
          </div>
        </div>
      </section>

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
              href="https://github.com/opentela/opentela"
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
