import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  isCatchAll,
  listMeshServices,
  listMeshCatalogue,
  type ServiceSummary,
  type MarketEntry,
} from './services';

afterEach(() => vi.restoreAllMocks());

// Shaped like the live /v1/services response.
const BODY = {
  services: [
    {
      name: 'flash-sandbox',
      models: [],
      identity_groups: ['all'],
      providers: 1,
      online: 1,
    },
    {
      name: 'llm',
      models: ['Llama-3', 'Qwen/Qwen3-8B'],
      identity_groups: ['model=Llama-3', 'model=Qwen/Qwen3-8B'],
      providers: 2,
      online: 1,
    },
  ],
};

describe('listMeshServices', () => {
  it('reads the catalogue without credentials', async () => {
    const fetchSpy = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(new Response(JSON.stringify(BODY), { status: 200 }));

    const services = await listMeshServices('https://api.example');

    expect(fetchSpy).toHaveBeenCalledWith('https://api.example/v1/services');
    // no second argument at all -> no Authorization header
    expect(fetchSpy.mock.calls[0]).toHaveLength(1);
    expect(services.map((s) => s.name)).toEqual(['flash-sandbox', 'llm']);
    expect(services[1].models).toEqual(['Llama-3', 'Qwen/Qwen3-8B']);
    expect(services[1].online).toBe(1);
  });

  it('tolerates a catalogue with no services', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ services: null }), { status: 200 }),
    );
    await expect(listMeshServices('https://api.example')).resolves.toEqual([]);
  });

  it('defaults missing fields rather than rendering undefined', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ services: [{ name: 'bare' }] }), { status: 200 }),
    );
    const [service] = await listMeshServices('https://api.example');
    expect(service).toEqual({
      name: 'bare',
      models: [],
      identity_groups: [],
      providers: 0,
      online: 0,
    });
  });

  it('throws on non-2xx', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 502 }));
    await expect(listMeshServices('https://api.example')).rejects.toThrow(/502/);
  });
});

describe('isCatchAll', () => {
  const base: ServiceSummary = {
    name: 'x',
    models: [],
    identity_groups: [],
    providers: 1,
    online: 1,
  };

  it('is true for a service advertising "all" and no models', () => {
    expect(isCatchAll({ ...base, identity_groups: ['all'] })).toBe(true);
  });

  it('is false once the service names models', () => {
    expect(
      isCatchAll({ ...base, models: ['Llama-3'], identity_groups: ['model=Llama-3'] }),
    ).toBe(false);
  });

  it('is false when it advertises nothing at all', () => {
    expect(isCatchAll(base)).toBe(false);
  });
});

const MARKET_BODY = {
  services: [
    { name: 'llm', models: ['Llama-3'], identity_groups: ['model=Llama-3'], providers: 3, online: 2 },
  ],
  market: [
    {
      service: 'llm',
      model: 'Llama-3',
      quoters: 3,
      input_per_million: { min: 1000, median: 1200, max: 1500 },
      cached_input_per_million: { min: 200, median: 250, max: 300 },
      output_per_million: { min: 3000, median: 3600, max: 4500 },
    },
    {
      // served but unpriced — quoters 0, zeroed triples
      service: 'llm',
      model: 'Qwen/Qwen3-8B',
      quoters: 0,
      input_per_million: { min: 0, median: 0, max: 0 },
      cached_input_per_million: { min: 0, median: 0, max: 0 },
      output_per_million: { min: 0, median: 0, max: 0 },
    },
  ],
};

describe('listMeshCatalogue', () => {
  it('returns services and market prices', async () => {
    const fetchSpy = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(new Response(JSON.stringify(MARKET_BODY), { status: 200 }));

    const catalogue = await listMeshCatalogue('https://api.example');

    expect(fetchSpy).toHaveBeenCalledWith('https://api.example/v1/services');
    expect(catalogue.services.map((s) => s.name)).toEqual(['llm']);
    expect(catalogue.market).toHaveLength(2);
    const llama = catalogue.market[0];
    expect(llama.quoters).toBe(3);
    expect(llama.input_per_million).toEqual({ min: 1000, median: 1200, max: 1500 });
    expect(llama.cached_input_per_million.median).toBe(250);
  });

  it('returns an empty market when billing is off', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ services: [], market: null }), { status: 200 }),
    );
    const catalogue = await listMeshCatalogue('https://api.example');
    expect(catalogue.services).toEqual([]);
    expect(catalogue.market).toEqual([]);
  });

  it('normalizes malformed market entries instead of throwing', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({ services: [], market: [{ service: 'llm' }] }),
        { status: 200 },
      ),
    );
    const catalogue = await listMeshCatalogue('https://api.example');
    expect(catalogue.market[0]).toEqual<MarketEntry>({
      service: 'llm',
      model: '',
      quoters: 0,
      input_per_million: { min: 0, median: 0, max: 0 },
      cached_input_per_million: { min: 0, median: 0, max: 0 },
      output_per_million: { min: 0, median: 0, max: 0 },
    });
  });

  it('listMeshServices stays a backward-compatible wrapper', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(MARKET_BODY), { status: 200 }),
    );
    const services = await listMeshServices('https://api.example');
    expect(services.map((s) => s.name)).toEqual(['llm']);
  });
});
