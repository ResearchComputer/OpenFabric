import {
  type BlockhashWithExpiryBlockHeight,
  Connection,
  PublicKey,
  Transaction,
} from '@solana/web3.js';
import {
  createAssociatedTokenAccountIdempotentInstruction,
  createTransferCheckedInstruction,
  getAssociatedTokenAddress,
} from '@solana/spl-token';
import { rawToUiAmount } from './format';
import type { SolanaWalletProvider } from './wallet';

export interface WalletBalances {
  sol: number;
  otela: string;
  ata: string;
}

export interface BuiltTransfer {
  transaction: Transaction;
  blockhash: BlockhashWithExpiryBlockHeight;
}

export function createConnection(rpcUrl: string): Connection {
  return new Connection(rpcUrl, 'confirmed');
}

export function describeSolanaError(error: unknown, rpcUrl: string): string {
  const message = error instanceof Error ? error.message : String(error);
  if (/(\b403\b|Access forbidden)/i.test(message)) {
    return (
      `${rpcUrl} refused the request (403). Public Solana endpoints commonly ` +
      `block browser traffic. Set a different RPC URL below.`
    );
  }
  return message;
}

export function parsePublicKey(value: string, label = 'address'): PublicKey {
  try {
    return new PublicKey(value.trim());
  } catch {
    throw new Error(`Invalid Solana ${label}`);
  }
}

export async function getWalletBalances(input: {
  connection: Connection;
  owner: PublicKey;
  mint: PublicKey;
  tokenProgramId: PublicKey;
  rpcUrl: string;
}): Promise<WalletBalances> {
  const ata = await getAssociatedTokenAddress(
    input.mint,
    input.owner,
    false,
    input.tokenProgramId,
  );

  let lamports: number;
  try {
    lamports = await input.connection.getBalance(input.owner, 'confirmed');
  } catch (error) {
    throw new Error(describeSolanaError(error, input.rpcUrl));
  }

  const tokenBalance = await input.connection
    .getTokenAccountBalance(ata, 'confirmed')
    .catch((error: unknown) => {
      const message = error instanceof Error ? error.message : String(error);
      if (/(\b403\b|Access forbidden)/i.test(message)) {
        throw new Error(describeSolanaError(error, input.rpcUrl));
      }
      return null;
    });

  return {
    sol: lamports / 1_000_000_000,
    otela:
      tokenBalance?.value.uiAmountString ??
      rawToUiAmount(0n, tokenBalance?.value.decimals ?? 9),
    ata: ata.toBase58(),
  };
}

export async function buildOtelaTransfer(input: {
  connection: Connection;
  owner: PublicKey;
  recipient: PublicKey;
  mint: PublicKey;
  amountRaw: bigint;
  decimals: number;
  tokenProgramId: PublicKey;
}): Promise<BuiltTransfer> {
  const sourceAta = await getAssociatedTokenAddress(
    input.mint,
    input.owner,
    false,
    input.tokenProgramId,
  );
  const recipientAta = await getAssociatedTokenAddress(
    input.mint,
    input.recipient,
    false,
    input.tokenProgramId,
  );

  const blockhash = await input.connection.getLatestBlockhash('confirmed');
  const transaction = new Transaction({
    feePayer: input.owner,
    blockhash: blockhash.blockhash,
    lastValidBlockHeight: blockhash.lastValidBlockHeight,
  });

  transaction.add(
    createAssociatedTokenAccountIdempotentInstruction(
      input.owner,
      recipientAta,
      input.recipient,
      input.mint,
      input.tokenProgramId,
    ),
    createTransferCheckedInstruction(
      sourceAta,
      input.mint,
      recipientAta,
      input.owner,
      input.amountRaw,
      input.decimals,
      [],
      input.tokenProgramId,
    ),
  );

  return { transaction, blockhash };
}

export async function sendWalletTransaction(input: {
  connection: Connection;
  provider: SolanaWalletProvider;
  built: BuiltTransfer;
}): Promise<string> {
  const { connection, provider, built } = input;

  if (provider.signAndSendTransaction) {
    const result = await provider.signAndSendTransaction(built.transaction);
    const signature = typeof result === 'string' ? result : result.signature;
    await connection.confirmTransaction(
      {
        signature,
        blockhash: built.blockhash.blockhash,
        lastValidBlockHeight: built.blockhash.lastValidBlockHeight,
      },
      'confirmed',
    );
    return signature;
  }

  if (!provider.signTransaction) {
    throw new Error('Connected wallet cannot sign transactions');
  }

  const signed = await provider.signTransaction(built.transaction);
  const signature = await connection.sendRawTransaction(signed.serialize());
  await connection.confirmTransaction(
    {
      signature,
      blockhash: built.blockhash.blockhash,
      lastValidBlockHeight: built.blockhash.lastValidBlockHeight,
    },
    'confirmed',
  );
  return signature;
}

export function explorerTransactionUrl(signature: string, cluster: string): string {
  const base = `https://explorer.solana.com/tx/${signature}`;
  return cluster === 'mainnet-beta'
    ? base
    : `${base}?cluster=${encodeURIComponent(cluster)}`;
}
