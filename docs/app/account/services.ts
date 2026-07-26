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
}

/** True when the service takes any request rather than named models. */
export function isCatchAll(service: ServiceSummary): boolean {
  return service.models.length === 0 && service.identity_groups.includes('all');
}

/** Fetch the catalogue. No credentials — this endpoint is public. */
export async function listMeshServices(
  baseUrl: string,
): Promise<ServiceSummary[]> {
  const res = await fetch(`${baseUrl}/v1/services`);
  if (!res.ok) throw new Error(`Could not load services: ${res.status}`);
  const body = (await res.json()) as CatalogueResponse;
  return (body.services ?? []).map((service) => ({
    name: service.name,
    models: service.models ?? [],
    identity_groups: service.identity_groups ?? [],
    providers: service.providers ?? 0,
    online: service.online ?? 0,
  }));
}
