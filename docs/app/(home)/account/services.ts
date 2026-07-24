export interface ServiceModel {
  id: string;
}

export async function listModels(
  baseUrl: string,
  apiKey: string,
): Promise<ServiceModel[]> {
  const res = await fetch(`${baseUrl}/v1/models`, {
    headers: { Authorization: `Bearer ${apiKey}` },
  });
  if (!res.ok) throw new Error(`Could not load services: ${res.status}`);
  const body = (await res.json()) as { data?: { id: string }[] };
  return (body.data ?? []).map((m) => ({ id: m.id }));
}
