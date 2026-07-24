const STORAGE_KEY = 'opentela-account-link';

export interface AccountLink {
  wallet: string;
  neonUserId: string;
  linkedAt: string;
}

export function readLink(): AccountLink | null {
  if (typeof window === 'undefined') return null;
  const raw = window.localStorage.getItem(STORAGE_KEY);
  if (!raw) return null;
  try {
    const parsed = JSON.parse(raw) as Partial<AccountLink>;
    if (!parsed.wallet || !parsed.neonUserId || !parsed.linkedAt) return null;
    return parsed as AccountLink;
  } catch {
    return null;
  }
}

export function writeLink(input: {
  wallet: string;
  neonUserId: string;
}): AccountLink {
  const link: AccountLink = {
    wallet: input.wallet,
    neonUserId: input.neonUserId,
    linkedAt: new Date().toISOString(),
  };
  window.localStorage.setItem(STORAGE_KEY, JSON.stringify(link));
  return link;
}

export function clearLink(): void {
  if (typeof window === 'undefined') return;
  window.localStorage.removeItem(STORAGE_KEY);
}
