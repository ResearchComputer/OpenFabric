/**
 * The mesh catalogue: what the network is serving right now.
 *
 * Served by the API as a distilled view — service names, the models they answer
 * for, and provider counts. No API key is needed to read it; a key is only
 * needed to call a service. The raw node table it is derived from stays behind
 * the key, since it carries operator wallet keys and peer addresses.
 */

export interface ServiceSummary {
  /** Service name, e.g. "llm" or "flash-sandbox". */
  name: string;
  /** Models this service answers for, from its `model=` identity groups. */
  models: string[];
  /** Identity groups as advertised; `["all"]` means it takes any request. */
  identity_groups: string[];
  /** Peers offering the service. */
  providers: number;
  /** Peers offering it that are currently connected. */
  online: number;
}

interface CatalogueResponse {
  services?: ServiceSummary[] | null;
  market?: MarketEntry[] | null;
}

/** True when the service takes any request rather than named models. */
export function isCatchAll(service: ServiceSummary): boolean {
  return service.models.length === 0 && service.identity_groups.includes('all');
}

/** Min/median/max published rate per million tokens for one tier. */
export interface PriceTriple {
  min: number;
  median: number;
  max: number;
}

/** One row of the on-chain price-discovery surface. `quoters` is the number of
 * peers with a live (unexpired) ask for this (service, model); zero means the
 * route is served but nobody has published a price yet. */
export interface MarketEntry {
  service: string;
  model: string;
  quoters: number;
  input_per_million: PriceTriple;
  cached_input_per_million: PriceTriple;
  output_per_million: PriceTriple;
}

/** The full catalogue: the distilled service list plus, when billing is
 * active, the live market prices. `market` is an empty array when the API is
 * running in `off` mode (no asks are published), so callers can render a
 * "pricing not enabled" state instead of crashing on undefined. */
export interface ServiceCatalogue {
  services: ServiceSummary[];
  market: MarketEntry[];
}

function normalizeTriple(value: unknown): PriceTriple {
  if (!isRecord(value)) return { min: 0, median: 0, max: 0 };
  return {
    min: readNumber(value.min) ?? 0,
    median: readNumber(value.median) ?? 0,
    max: readNumber(value.max) ?? 0,
  };
}

function normalizeMarketEntry(value: unknown): MarketEntry {
  if (!isRecord(value)) return emptyMarketEntry('', '');
  return {
    service: readString(value.service) ?? '',
    model: readString(value.model) ?? '',
    quoters: readNumber(value.quoters) ?? 0,
    input_per_million: normalizeTriple(value.input_per_million),
    cached_input_per_million: normalizeTriple(value.cached_input_per_million),
    output_per_million: normalizeTriple(value.output_per_million),
  };
}

function emptyMarketEntry(service: string, model: string): MarketEntry {
  const zero = { min: 0, median: 0, max: 0 };
  return {
    service,
    model,
    quoters: 0,
    input_per_million: zero,
    cached_input_per_million: zero,
    output_per_million: zero,
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}

function readString(value: unknown): string | null {
  return typeof value === 'string' ? value : null;
}

function readNumber(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null;
}

/** Fetch the catalogue. No credentials — this endpoint is public. */
export async function listMeshServices(
  baseUrl: string,
): Promise<ServiceSummary[]> {
  const catalogue = await listMeshCatalogue(baseUrl);
  return catalogue.services;
}

/** Fetch the full catalogue (services + live market prices). The market array
 * is empty when the API is running in `off` mode; callers that only need the
 * service list can keep using {@link listMeshServices}. */
export async function listMeshCatalogue(
  baseUrl: string,
): Promise<ServiceCatalogue> {
  const res = await fetch(`${baseUrl}/v1/services`);
  if (!res.ok) throw new Error(`Could not load services: ${res.status}`);
  const body = (await res.json()) as CatalogueResponse;
  const services = (body.services ?? []).map((service) => ({
    name: service.name,
    models: service.models ?? [],
    identity_groups: service.identity_groups ?? [],
    providers: service.providers ?? 0,
    online: service.online ?? 0,
  }));
  const market = (body.market ?? []).map(normalizeMarketEntry);
  return { services, market };
}
