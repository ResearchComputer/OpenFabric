import { afterEach, describe, expect, it, vi } from 'vitest';
import { isCatchAll, listMeshServices, type ServiceSummary } from './services';

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
