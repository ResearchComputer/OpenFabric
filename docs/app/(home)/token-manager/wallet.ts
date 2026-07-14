import bs58 from 'bs58';
import type { Transaction } from '@solana/web3.js';

export interface WalletPublicKey {
  toBase58(): string;
}

export interface SolanaWalletProvider {
  isPhantom?: boolean;
  publicKey?: WalletPublicKey | null;
  connect(options?: { onlyIfTrusted?: boolean }): Promise<{ publicKey: WalletPublicKey }>;
  disconnect(): Promise<void>;
  signMessage?(
    message: Uint8Array,
    display?: 'utf8' | 'hex',
  ): Promise<{ signature: Uint8Array } | Uint8Array>;
  signTransaction?(transaction: Transaction): Promise<Transaction>;
  signAndSendTransaction?(
    transaction: Transaction,
  ): Promise<{ signature: string } | string>;
}

declare global {
  interface Window {
    solana?: SolanaWalletProvider;
    phantom?: {
      solana?: SolanaWalletProvider;
    };
  }
}

export function getInjectedWallet(): SolanaWalletProvider | null {
  if (typeof window === 'undefined') return null;
  return window.phantom?.solana ?? window.solana ?? null;
}

export function publicKeyString(provider: SolanaWalletProvider): string | null {
  return provider.publicKey?.toBase58() ?? null;
}

export async function signWalletMessage(
  provider: SolanaWalletProvider,
  message: string,
): Promise<string> {
  if (!provider.signMessage) {
    throw new Error('Connected wallet does not support message signing');
  }

  const encoded = new TextEncoder().encode(message);
  const result = await provider.signMessage(encoded, 'utf8');
  const signature = result instanceof Uint8Array ? result : result.signature;
  return bs58.encode(signature);
}
