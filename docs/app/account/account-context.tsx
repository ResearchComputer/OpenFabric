'use client';

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import { Buffer } from 'buffer';
import { PublicKey, type Connection } from '@solana/web3.js';
import { tokenManagerConfig } from './config';
import { authClient, getAuthJwt, AuthTokenError } from './neon-auth';
import {
  listManageKeys,
  listManageInstances,
  listManageWallets,
  type CreatedManageKey,
  type ManageIdentitySnapshot,
  type ManageInstance,
  type ManageKey,
  type ManageWallet,
} from './manage-api';
import {
  createConnection,
  getWalletBalances,
  type WalletBalances,
} from './solana';
import {
  getInjectedWallet,
  publicKeyString,
  type SolanaWalletProvider,
} from './wallet';

if (typeof globalThis !== 'undefined' && !('Buffer' in globalThis)) {
  (globalThis as typeof globalThis & { Buffer: typeof Buffer }).Buffer = Buffer;
}

const RPC_STORAGE_KEY = 'opentela-token-manager-rpc';

export type NoticeKind = 'info' | 'success' | 'error';

export interface Notice {
  kind: NoticeKind;
  message: string;
}

export interface NeonUser {
  id: string;
  email?: string;
}

interface AccountValue {
  mounted: boolean;
  neonUser: NeonUser | null;
  sessionLoading: boolean;
  sessionError: string | null;
  retrySession: () => void;
  signOut: () => void;

  provider: SolanaWalletProvider | null;
  wallet: string | null;
  balances: WalletBalances | null;
  connection: Connection;
  rpcUrl: string;
  connectWallet: () => Promise<void>;
  disconnectWallet: () => Promise<void>;
  refreshBalances: (walletAddress?: string | null) => Promise<void>;
  applyRpcUrl: (nextRpcUrl: string) => Promise<void>;
  resetRpcUrl: () => void;

  skKeys: ManageKey[];
  refreshSkKeys: () => Promise<void>;
  linkedWallets: ManageWallet[];
  walletBindingsLoaded: boolean;
  walletBindingsError: string | null;
  walletIdentity: ManageIdentitySnapshot | null;
  refreshLinkedWallets: () => Promise<void>;
  managedInstances: ManageInstance[];
  instancesLoaded: boolean;
  instancesError: string | null;
  refreshManagedInstances: () => Promise<void>;
  handleApiError: (error: unknown) => void;
  lastCreatedSk: CreatedManageKey | null;
  setLastCreatedSk: (key: CreatedManageKey | null) => void;

  notice: Notice | null;
  showNotice: (kind: NoticeKind, message: string) => void;
  pending: string | null;
  setPending: (key: string | null) => void;
  releasePending: (key: string) => void;
}

const AccountContext = createContext<AccountValue | null>(null);

export function useAccount(): AccountValue {
  const ctx = useContext(AccountContext);
  if (!ctx) {
    throw new Error('useAccount must be used inside <AccountProvider>');
  }
  return ctx;
}

function sessionErrorMessage(error: unknown): string {
  const detail = error as { status?: number; message?: string; statusText?: string };
  return (
    detail?.message ??
    detail?.statusText ??
    'The sign-in server could not be reached'
  );
}

// Subscribes to the live Neon session so sign-in/sign-out reflect in place.
// Rendered only when authClient is non-null (browser + configured).
function NeonSessionSync({
  client,
  onUser,
  onLoading,
  onSession,
}: {
  client: NonNullable<typeof authClient>;
  onUser: (user: NeonUser | null) => void;
  onLoading: (loading: boolean) => void;
  onSession: (error: string | null, refetch: () => void) => void;
}) {
  const { data, isPending, error, refetch } = client.useSession();
  const userId = data?.user?.id;
  const userEmail = data?.user?.email;
  const failure = error ? sessionErrorMessage(error) : null;

  useEffect(() => {
    // A failed session check reports no data, exactly like a signed-out one.
    // Only the second is a reason to drop the user: doing it for the first
    // swaps the console for the sign-in screen on any network blip.
    if (failure) return;
    onUser(userId ? { id: userId, email: userEmail ?? undefined } : null);
  }, [failure, userId, userEmail, onUser]);
  useEffect(() => {
    onLoading(Boolean(isPending));
  }, [isPending, onLoading]);
  useEffect(() => {
    onSession(failure, refetch);
  }, [failure, refetch, onSession]);
  return null;
}

export function AccountProvider({ children }: { children: ReactNode }) {
  const [mounted, setMounted] = useState(false);
  const [neonUser, setNeonUser] = useState<NeonUser | null>(null);
  const [sessionLoading, setSessionLoading] = useState(true);
  const [sessionError, setSessionError] = useState<string | null>(null);
  const sessionRefetch = useRef<(() => void) | null>(null);
  const [provider, setProvider] = useState<SolanaWalletProvider | null>(null);
  const [wallet, setWallet] = useState<string | null>(null);
  const [balances, setBalances] = useState<WalletBalances | null>(null);
  const [rpcUrl, setRpcUrl] = useState(tokenManagerConfig.solanaRpcUrl);
  const [skKeys, setSkKeys] = useState<ManageKey[]>([]);
  const [linkedWallets, setLinkedWallets] = useState<ManageWallet[]>([]);
  const [walletBindingsLoaded, setWalletBindingsLoaded] = useState(false);
  const [walletBindingsError, setWalletBindingsError] = useState<string | null>(null);
  const [walletIdentity, setWalletIdentity] = useState<ManageIdentitySnapshot | null>(
    null,
  );
  const [managedInstances, setManagedInstances] = useState<ManageInstance[]>([]);
  const [instancesLoaded, setInstancesLoaded] = useState(false);
  const [instancesError, setInstancesError] = useState<string | null>(null);
  const [lastCreatedSk, setLastCreatedSk] = useState<CreatedManageKey | null>(null);
  const [notice, setNotice] = useState<Notice | null>(null);
  const [pending, setPending] = useState<string | null>(null);

  const connection = useMemo(() => createConnection(rpcUrl), [rpcUrl]);

  useEffect(() => {
    setMounted(true);
  }, []);

  useEffect(() => {
    if (!authClient) setSessionLoading(false);
  }, []);

  const onSession = useCallback((error: string | null, refetch: () => void) => {
    sessionRefetch.current = refetch;
    setSessionError(error);
  }, []);

  const retrySession = useCallback(() => {
    sessionRefetch.current?.();
  }, []);

  useEffect(() => {
    const injected = getInjectedWallet();
    setProvider(injected);
    if (injected) setWallet(publicKeyString(injected));

    const savedRpcUrl = window.localStorage.getItem(RPC_STORAGE_KEY);
    if (savedRpcUrl) setRpcUrl(savedRpcUrl);
  }, []);

  const showNotice = useCallback((kind: NoticeKind, message: string) => {
    setNotice({ kind, message });
  }, []);

  const releasePending = useCallback((key: string) => {
    setPending((prev) => (prev === key ? null : prev));
  }, []);

  const refreshBalances = useCallback(
    async (walletAddress: string | null = wallet) => {
      if (!walletAddress) return;
      const owner = new PublicKey(walletAddress);
      const nextBalances = await getWalletBalances({
        connection,
        owner,
        mint: tokenManagerConfig.otelaMint,
        tokenProgramId: tokenManagerConfig.tokenProgramId,
        rpcUrl,
      });
      setBalances(nextBalances);
    },
    [connection, rpcUrl, wallet],
  );

  const refreshSkKeys = useCallback(async () => {
    if (!neonUser) return;
    const jwt = await getAuthJwt();
    setSkKeys(await listManageKeys(tokenManagerConfig.apiBaseUrl, jwt));
  }, [neonUser]);

  const refreshLinkedWallets = useCallback(async () => {
    if (!neonUser) return;
    setWalletBindingsError(null);
    try {
      const jwt = await getAuthJwt();
      const snapshot = await listManageWallets(tokenManagerConfig.apiBaseUrl, jwt);
      setLinkedWallets(snapshot.wallets);
      if (snapshot.identity) setWalletIdentity(snapshot.identity);
      setWalletBindingsLoaded(true);
    } catch (error) {
      setWalletBindingsLoaded(true);
      setWalletBindingsError(error instanceof Error ? error.message : String(error));
      throw error;
    }
  }, [neonUser]);

  const refreshManagedInstances = useCallback(async () => {
    if (!neonUser) return;
    setInstancesError(null);
    try {
      const jwt = await getAuthJwt();
      const snapshot = await listManageInstances(tokenManagerConfig.apiBaseUrl, jwt);
      setManagedInstances(snapshot.instances);
      if (snapshot.identity) setWalletIdentity(snapshot.identity);
      setInstancesLoaded(true);
    } catch (error) {
      setInstancesLoaded(true);
      setInstancesError(error instanceof Error ? error.message : String(error));
      throw error;
    }
  }, [neonUser]);

  /**
   * Surface an API failure. A session that cannot be exchanged for a JWT is not
   * usable, even though the cached session still renders as signed in — drop it
   * so the gate returns to sign-in instead of stranding the user on a dashboard
   * where every action fails.
   */
  const handleApiError = useCallback((error: unknown) => {
    if (error instanceof AuthTokenError) {
      showNotice('error', `${error.message}`);
      authClient?.signOut().finally(() => {
        setNeonUser(null);
        setSkKeys([]);
        setLinkedWallets([]);
        setWalletIdentity(null);
        setManagedInstances([]);
        setLastCreatedSk(null);
      });
      return;
    }
    showNotice('error', error instanceof Error ? error.message : String(error));
  }, [showNotice]);

  useEffect(() => {
    if (!wallet) return;
    refreshBalances().catch((error: unknown) => {
      showNotice('error', error instanceof Error ? error.message : String(error));
    });
  }, [refreshBalances, showNotice, wallet]);

  useEffect(() => {
    if (!neonUser) {
      setSkKeys([]);
      setLinkedWallets([]);
      setWalletBindingsLoaded(false);
      setWalletBindingsError(null);
      setWalletIdentity(null);
      setManagedInstances([]);
      setInstancesLoaded(false);
      setInstancesError(null);
      setLastCreatedSk(null);
      return;
    }
    refreshSkKeys().catch((error: unknown) => handleApiError(error));
    refreshLinkedWallets().catch((error: unknown) => handleApiError(error));
    refreshManagedInstances().catch((error: unknown) => handleApiError(error));
  }, [
    handleApiError,
    neonUser,
    refreshLinkedWallets,
    refreshManagedInstances,
    refreshSkKeys,
  ]);

  const connectWallet = useCallback(async () => {
    if (!provider) {
      showNotice('error', 'No Solana browser wallet detected');
      return;
    }
    setPending('connect');
    try {
      const result = await provider.connect();
      const nextWallet = result.publicKey.toBase58();
      setWallet(nextWallet);
      await refreshBalances(nextWallet);
      showNotice('success', 'Wallet connected');
    } catch (error) {
      showNotice('error', error instanceof Error ? error.message : String(error));
    } finally {
      releasePending('connect');
    }
  }, [provider, refreshBalances, showNotice, releasePending]);

  const disconnectWallet = useCallback(async () => {
    if (!provider) return;
    setPending('disconnect');
    try {
      await provider.disconnect();
      setWallet(null);
      setBalances(null);
      showNotice('info', 'Wallet disconnected');
    } catch (error) {
      showNotice('error', error instanceof Error ? error.message : String(error));
    } finally {
      releasePending('disconnect');
    }
  }, [provider, showNotice, releasePending]);

  const applyRpcUrl = useCallback(
    async (nextRpcUrl: string) => {
      const trimmed = nextRpcUrl.trim();
      try {
        const parsed = new URL(trimmed);
        if (!['http:', 'https:'].includes(parsed.protocol)) {
          throw new Error('RPC URL must start with http:// or https://');
        }
      } catch (error) {
        showNotice('error', error instanceof Error ? error.message : 'Invalid RPC URL');
        return;
      }

      window.localStorage.setItem(RPC_STORAGE_KEY, trimmed);
      setRpcUrl(trimmed);
      setBalances(null);
      showNotice('info', 'RPC endpoint updated');

      if (wallet) {
        setPending('refresh-balances');
        try {
          const owner = new PublicKey(wallet);
          const nextBalances = await getWalletBalances({
            connection: createConnection(trimmed),
            owner,
            mint: tokenManagerConfig.otelaMint,
            tokenProgramId: tokenManagerConfig.tokenProgramId,
            rpcUrl: trimmed,
          });
          setBalances(nextBalances);
        } catch (error) {
          showNotice('error', error instanceof Error ? error.message : String(error));
        } finally {
          releasePending('refresh-balances');
        }
      }
    },
    [showNotice, wallet, releasePending],
  );

  const resetRpcUrl = useCallback(() => {
    window.localStorage.removeItem(RPC_STORAGE_KEY);
    setRpcUrl(tokenManagerConfig.solanaRpcUrl);
    setBalances(null);
    showNotice('info', 'RPC endpoint reset to the site default');
  }, [showNotice]);

  const signOut = useCallback(() => {
    authClient?.signOut().finally(() => {
      setNeonUser(null);
      setSkKeys([]);
      setLinkedWallets([]);
      setWalletBindingsLoaded(false);
      setWalletBindingsError(null);
      setWalletIdentity(null);
      setManagedInstances([]);
      setInstancesLoaded(false);
      setInstancesError(null);
      setLastCreatedSk(null);
    });
  }, []);

  const value: AccountValue = {
    mounted,
    neonUser,
    sessionLoading,
    sessionError,
    retrySession,
    signOut,
    provider,
    wallet,
    balances,
    connection,
    rpcUrl,
    connectWallet,
    disconnectWallet,
    refreshBalances,
    applyRpcUrl,
    resetRpcUrl,
    skKeys,
    refreshSkKeys,
    linkedWallets,
    walletBindingsLoaded,
    walletBindingsError,
    walletIdentity,
    refreshLinkedWallets,
    managedInstances,
    instancesLoaded,
    instancesError,
    refreshManagedInstances,
    handleApiError,
    lastCreatedSk,
    setLastCreatedSk,
    notice,
    showNotice,
    pending,
    setPending,
    releasePending,
  };

  return (
    <AccountContext.Provider value={value}>
      {mounted && authClient ? (
        <NeonSessionSync
          client={authClient}
          onUser={setNeonUser}
          onLoading={setSessionLoading}
          onSession={onSession}
        />
      ) : null}
      {children}
    </AccountContext.Provider>
  );
}
