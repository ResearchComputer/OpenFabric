import { beforeAll } from 'vitest';

beforeAll(() => {
  if (typeof window !== 'undefined' && !window.localStorage) {
    // Initialize localStorage if not available
    const store: Record<string, string> = {};
    window.localStorage = {
      getItem: (key: string) => store[key] || null,
      setItem: (key: string, value: string) => {
        store[key] = value;
      },
      removeItem: (key: string) => {
        delete store[key];
      },
      clear: () => {
        Object.keys(store).forEach(key => delete store[key]);
      },
      length: Object.keys(store).length,
      key: (index: number) => Object.keys(store)[index] || null,
    } as any;
  }
});
