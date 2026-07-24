import { afterEach, describe, expect, it, vi } from 'vitest';
import { listModels } from './services';

afterEach(() => vi.restoreAllMocks());

describe('listModels', () => {
  it('parses OpenAI-style {data:[...]}', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ object: 'list', data: [{ id: 'Qwen/Qwen3-8B' }] }), { status: 200 }));
    const models = await listModels('https://api.example', 'sk-x');
    expect(models).toEqual([{ id: 'Qwen/Qwen3-8B' }]);
  });

  it('throws on non-2xx', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 401 }));
    await expect(listModels('https://api.example', 'sk-x')).rejects.toThrow();
  });
});
