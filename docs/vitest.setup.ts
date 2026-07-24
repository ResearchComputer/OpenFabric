// jsdom, as instantiated by vitest's `jsdom` environment, does NOT expose the
// Web Storage API (`window.localStorage` is `undefined` — verified empirically).
// Provide a spec-correct in-memory Storage so tests exercise real
// get/set/remove/clear/length semantics against the ambient browser API. The
// code under test still runs for real; only the browser-provided storage is shimmed.
class MemoryStorage {
  private store = new Map<string, string>();

  get length(): number {
    return this.store.size;
  }

  clear(): void {
    this.store.clear();
  }

  getItem(key: string): string | null {
    return this.store.has(key) ? this.store.get(key)! : null;
  }

  key(index: number): string | null {
    return Array.from(this.store.keys())[index] ?? null;
  }

  removeItem(key: string): void {
    this.store.delete(key);
  }

  setItem(key: string, value: string): void {
    this.store.set(key, String(value));
  }
}

if (typeof window !== 'undefined' && !window.localStorage) {
  Object.defineProperty(window, 'localStorage', {
    configurable: true,
    value: new MemoryStorage() as unknown as Storage,
  });
}
