/**
 * OpenTela Ownership Tools – SPL Token Helper Functions
 *
 * Shared utilities for interacting with SPL tokens on Solana.  These
 * helpers are consumed by the individual command scripts (create-token,
 * mint-tokens, transfer-tokens, etc.) and encapsulate the low-level
 * plumbing so that each script can stay focused on its own workflow.
 *
 * All functions accept explicit `Connection` / `Keypair` arguments so
 * they remain pure and testable without relying on global state.
 */

import {
  Connection,
  Keypair,
  PublicKey,
  SystemProgram,
  Transaction,
  TransactionInstruction,
  sendAndConfirmTransaction,
  LAMPORTS_PER_SOL,
} from "@solana/web3.js";

import {
  TOKEN_PROGRAM_ID,
  getAssociatedTokenAddressSync,
  createAssociatedTokenAccountInstruction,
  createInitializeMint2Instruction,
  createMintToInstruction,
  createTransferInstruction,
  createBurnInstruction,
  createSetAuthorityInstruction,
  AuthorityType,
  getAccount,
  getMint,
  getOrCreateAssociatedTokenAccount,
  TokenAccountNotFoundError,
  TokenInvalidAccountOwnerError,
  type Account as TokenAccount,
  type Mint as MintAccount,
} from "@solana/spl-token";

import * as log from "./log.js";

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/** Minimum rent-exempt balance for a Mint account (82 bytes). */
const MINT_ACCOUNT_SIZE = 82;

// ---------------------------------------------------------------------------
// Associated Token Account (ATA) helpers
// ---------------------------------------------------------------------------

/**
 * Derive the Associated Token Account address for an owner + mint pair.
 *
 * This is a pure derivation — it does not touch the network.
 */
export function deriveATA(owner: PublicKey, mint: PublicKey): PublicKey {
  return getAssociatedTokenAddressSync(mint, owner, true);
}

/**
 * Get or create the Associated Token Account for `owner` and `mint`.
 *
 * If the ATA already exists its info is returned directly.  Otherwise a
 * transaction is sent to create it, funded by `payer`.
 *
 * @returns The token account info (address, balance, etc.).
 */
export async function getOrCreateATA(
  connection: Connection,
  payer: Keypair,
  mint: PublicKey,
  owner: PublicKey,
): Promise<TokenAccount> {
  const ata = deriveATA(owner, mint);
  log.debug(`Derived ATA for ${owner.toBase58()}: ${ata.toBase58()}`);

  try {
    const account = await getAccount(connection, ata, "confirmed");
    log.debug(`ATA already exists with balance ${account.amount.toString()}`);
    return account;
  } catch (err: unknown) {
    if (
      err instanceof TokenAccountNotFoundError ||
      err instanceof TokenInvalidAccountOwnerError
    ) {
      log.debug("ATA does not exist – creating…");
      const account = await getOrCreateAssociatedTokenAccount(
        connection,
        payer,
        mint,
        owner,
        true, // allowOwnerOffCurve
      );
      log.debug(`ATA created: ${account.address.toBase58()}`);
      return account;
    }
    throw err;
  }
}

/**
 * Build (but do not send) the instruction to create an ATA.
 *
 * Useful when you want to batch this instruction into a larger
 * transaction.
 */
export function buildCreateATAInstruction(
  payer: PublicKey,
  mint: PublicKey,
  owner: PublicKey,
): { instruction: TransactionInstruction; ata: PublicKey } {
  const ata = deriveATA(owner, mint);
  const instruction = createAssociatedTokenAccountInstruction(
    payer,
    ata,
    owner,
    mint,
  );
  return { instruction, ata };
}

/**
 * Check whether an ATA exists on-chain.
 */
export async function ataExists(
  connection: Connection,
  owner: PublicKey,
  mint: PublicKey,
): Promise<boolean> {
  const ata = deriveATA(owner, mint);
  try {
    await getAccount(connection, ata, "confirmed");
    return true;
  } catch (err: unknown) {
    if (
      err instanceof TokenAccountNotFoundError ||
      err instanceof TokenInvalidAccountOwnerError
    ) {
      return false;
    }
    throw err;
  }
}

// ---------------------------------------------------------------------------
// Mint helpers
// ---------------------------------------------------------------------------

/**
 * Fetch on-chain Mint account data.  Throws if the mint does not exist.
 */
export async function fetchMint(
  connection: Connection,
  mint: PublicKey,
): Promise<MintAccount> {
  return getMint(connection, mint, "confirmed");
}

/**
 * Check whether a mint account exists on-chain.
 */
export async function mintExists(
  connection: Connection,
  mint: PublicKey,
): Promise<boolean> {
  try {
    await getMint(connection, mint, "confirmed");
    return true;
  } catch {
    return false;
  }
}

/**
 * Result returned by {@link createSPLToken}.
 */
export interface CreateTokenResult {
  /** The mint public key. */
  mint: PublicKey;

  /** The keypair of the mint account (caller may need to persist this). */
  mintKeypair: Keypair;

  /** The transaction signature. */
  signature: string;
}

/**
 * Create a brand-new SPL Token mint.
 *
 * This uses the low-level Token Program instructions directly (not
 * Anchor) so it works on any cluster without a deployed Anchor program.
 *
 * The caller is set as both the **mint authority** and the **freeze
 * authority** (freeze authority can optionally be `null`).
 *
 * @param connection   - Solana RPC connection.
 * @param payer        - Fee-payer and mint/freeze authority.
 * @param decimals     - Number of decimal places (default 9).
 * @param mintKeypair  - Optional pre-generated keypair for the mint
 *                       account.  When omitted a fresh keypair is
 *                       generated.
 * @param freezeAuthority - Freeze authority, or `null` to disable.
 *                          Defaults to `payer.publicKey`.
 */
export async function createSPLToken(
  connection: Connection,
  payer: Keypair,
  decimals: number = 9,
  mintKeypair?: Keypair,
  freezeAuthority?: PublicKey | null,
): Promise<CreateTokenResult> {
  const mint = mintKeypair ?? Keypair.generate();

  const lamports =
    await connection.getMinimumBalanceForRentExemption(MINT_ACCOUNT_SIZE);

  const resolvedFreeze =
    freezeAuthority === undefined ? payer.publicKey : freezeAuthority;

  const transaction = new Transaction().add(
    // 1. Allocate the mint account.
    SystemProgram.createAccount({
      fromPubkey: payer.publicKey,
      newAccountPubkey: mint.publicKey,
      lamports,
      space: MINT_ACCOUNT_SIZE,
      programId: TOKEN_PROGRAM_ID,
    }),
    // 2. Initialize it as a Mint.
    createInitializeMint2Instruction(
      mint.publicKey,
      decimals,
      payer.publicKey, // mint authority
      resolvedFreeze, // freeze authority
    ),
  );

  const signature = await sendAndConfirmTransaction(
    connection,
    transaction,
    [payer, mint],
    { commitment: "confirmed" },
  );

  return { mint: mint.publicKey, mintKeypair: mint, signature };
}

/**
 * Ensure a mint exists; if `mintAddress` is provided and already on-chain,
 * return its info.  If it is empty / missing, create a new one.
 *
 * This is a convenience wrapper for scripts that support both "create a
 * new token" and "use an existing one" flows.
 */
export async function ensureMint(
  connection: Connection,
  payer: Keypair,
  mintAddress: string | undefined,
  decimals: number = 9,
): Promise<{ mint: PublicKey; created: boolean; signature?: string }> {
  if (mintAddress) {
    const mint = new PublicKey(mintAddress);
    const exists = await mintExists(connection, mint);
    if (!exists) {
      throw new Error(
        `Mint ${mintAddress} was specified but does not exist on-chain.`,
      );
    }
    return { mint, created: false };
  }

  log.info("No TOKEN_MINT configured – creating a new SPL token…");
  const result = await createSPLToken(connection, payer, decimals);
  return { mint: result.mint, created: true, signature: result.signature };
}

// ---------------------------------------------------------------------------
// Minting helpers
// ---------------------------------------------------------------------------

/**
 * Mint SPL tokens to a destination wallet.
 *
 * The `authority` must be the current mint authority of `mint`.  The
 * destination ATA is created automatically if it does not exist.
 *
 * @param connection  - Solana RPC connection.
 * @param authority   - Mint authority keypair (also pays for ATA creation).
 * @param mint        - Mint public key.
 * @param destination - Owner of the receiving wallet (the ATA is derived
 *                      from this + `mint`).
 * @param amount      - Raw token amount (i.e. value × 10^decimals).
 */
export async function mintTokensTo(
  connection: Connection,
  authority: Keypair,
  mint: PublicKey,
  destination: PublicKey,
  amount: bigint,
): Promise<{ ata: PublicKey; signature: string }> {
  // Ensure the destination ATA exists.
  const ataAccount = await getOrCreateATA(
    connection,
    authority,
    mint,
    destination,
  );

  const transaction = new Transaction().add(
    createMintToInstruction(
      mint,
      ataAccount.address,
      authority.publicKey,
      amount,
    ),
  );

  const signature = await sendAndConfirmTransaction(
    connection,
    transaction,
    [authority],
    { commitment: "confirmed" },
  );

  return { ata: ataAccount.address, signature };
}

/**
 * Convert a human-readable token amount (e.g. 100.5) to the raw integer
 * representation used on-chain.
 *
 * @param amount   - The amount in whole tokens (may be fractional).
 * @param decimals - The token's decimal places.
 */
export function toRawAmount(amount: number, decimals: number): bigint {
  // Use string math to avoid floating-point rounding surprises.
  const parts = amount.toString().split(".");
  const whole = parts[0] ?? "0";
  let frac = parts[1] ?? "";

  if (frac.length > decimals) {
    throw new Error(
      `Amount ${amount} has more than ${decimals} decimal places.`,
    );
  }

  frac = frac.padEnd(decimals, "0");
  return BigInt(whole + frac);
}

/**
 * Convert a raw on-chain amount to a human-readable number.
 */
export function fromRawAmount(raw: bigint, decimals: number): number {
  const str = raw.toString().padStart(decimals + 1, "0");
  const whole = str.slice(0, str.length - decimals);
  const frac = str.slice(str.length - decimals);
  return parseFloat(`${whole}.${frac}`);
}

// ---------------------------------------------------------------------------
// Transfer helpers
// ---------------------------------------------------------------------------

/**
 * Transfer SPL tokens between two wallets.
 *
 * Both the source and destination ATAs are resolved automatically.
 * The destination ATA is created if it does not exist.
 *
 * @param connection - Solana RPC connection.
 * @param sender     - The sending wallet keypair (owner of the source ATA
 *                     and fee-payer).
 * @param mint       - Token mint.
 * @param recipient  - Recipient wallet public key.
 * @param amount     - Raw token amount.
 */
export async function transferTokens(
  connection: Connection,
  sender: Keypair,
  mint: PublicKey,
  recipient: PublicKey,
  amount: bigint,
): Promise<{ sourceAta: PublicKey; destAta: PublicKey; signature: string }> {
  const sourceAta = deriveATA(sender.publicKey, mint);
  const destAccount = await getOrCreateATA(connection, sender, mint, recipient);

  const transaction = new Transaction().add(
    createTransferInstruction(
      sourceAta,
      destAccount.address,
      sender.publicKey,
      amount,
    ),
  );

  const signature = await sendAndConfirmTransaction(
    connection,
    transaction,
    [sender],
    { commitment: "confirmed" },
  );

  return { sourceAta, destAta: destAccount.address, signature };
}

// ---------------------------------------------------------------------------
// Burn helpers
// ---------------------------------------------------------------------------

/**
 * Burn SPL tokens from a wallet.
 *
 * @param connection - Solana RPC connection.
 * @param owner      - Token account owner and fee-payer.
 * @param mint       - Token mint.
 * @param amount     - Raw token amount to burn.
 */
export async function burnTokens(
  connection: Connection,
  owner: Keypair,
  mint: PublicKey,
  amount: bigint,
): Promise<{ ata: PublicKey; signature: string }> {
  const ata = deriveATA(owner.publicKey, mint);

  const transaction = new Transaction().add(
    createBurnInstruction(ata, mint, owner.publicKey, amount),
  );

  const signature = await sendAndConfirmTransaction(
    connection,
    transaction,
    [owner],
    { commitment: "confirmed" },
  );

  return { ata, signature };
}

// ---------------------------------------------------------------------------
// Authority helpers
// ---------------------------------------------------------------------------

/**
 * Revoke (disable) the mint authority on a token.
 *
 * After this call no further tokens can ever be minted.  This is
 * irreversible.
 *
 * @param connection - Solana RPC connection.
 * @param authority  - Current mint authority keypair.
 * @param mint       - Token mint.
 */
export async function revokeMintAuthority(
  connection: Connection,
  authority: Keypair,
  mint: PublicKey,
): Promise<string> {
  const transaction = new Transaction().add(
    createSetAuthorityInstruction(
      mint,
      authority.publicKey,
      AuthorityType.MintTokens,
      null, // new authority = none → disabled
    ),
  );

  const signature = await sendAndConfirmTransaction(
    connection,
    transaction,
    [authority],
    { commitment: "confirmed" },
  );

  return signature;
}

/**
 * Revoke (disable) the freeze authority on a token.
 *
 * @param connection - Solana RPC connection.
 * @param authority  - Current freeze authority keypair.
 * @param mint       - Token mint.
 */
export async function revokeFreezeAuthority(
  connection: Connection,
  authority: Keypair,
  mint: PublicKey,
): Promise<string> {
  const transaction = new Transaction().add(
    createSetAuthorityInstruction(
      mint,
      authority.publicKey,
      AuthorityType.FreezeAccount,
      null,
    ),
  );

  const signature = await sendAndConfirmTransaction(
    connection,
    transaction,
    [authority],
    { commitment: "confirmed" },
  );

  return signature;
}

/**
 * Transfer the mint authority to a new public key.
 *
 * @param connection      - Solana RPC connection.
 * @param currentAuthority - The current mint authority keypair.
 * @param mint             - Token mint.
 * @param newAuthority     - The new mint authority public key.
 */
export async function transferMintAuthority(
  connection: Connection,
  currentAuthority: Keypair,
  mint: PublicKey,
  newAuthority: PublicKey,
): Promise<string> {
  const transaction = new Transaction().add(
    createSetAuthorityInstruction(
      mint,
      currentAuthority.publicKey,
      AuthorityType.MintTokens,
      newAuthority,
    ),
  );

  const signature = await sendAndConfirmTransaction(
    connection,
    transaction,
    [currentAuthority],
    { commitment: "confirmed" },
  );

  return signature;
}

// ---------------------------------------------------------------------------
// Ownership verification helpers
// ---------------------------------------------------------------------------

/**
 * Result of an ownership check.
 */
export interface OwnershipCheckResult {
  /** Whether the wallet meets the minimum balance requirement. */
  isOwner: boolean;

  /** The wallet's raw token balance. */
  rawBalance: bigint;

  /** The wallet's balance in human-readable (UI) form. */
  uiBalance: number;

  /** The token's decimal places. */
  decimals: number;

  /** The ATA address that was checked. */
  ata: PublicKey;
}

/**
 * Check whether `owner` holds at least `minBalance` tokens of `mint`.
 *
 * @param connection - Solana RPC connection.
 * @param owner      - The wallet to check.
 * @param mint       - The SPL token mint.
 * @param minBalance - Minimum balance in whole tokens (default 1).
 */
export async function verifyOwnership(
  connection: Connection,
  owner: PublicKey,
  mint: PublicKey,
  minBalance: number = 1,
): Promise<OwnershipCheckResult> {
  const ata = deriveATA(owner, mint);
  const mintInfo = await fetchMint(connection, mint);

  let rawBalance = BigInt(0);
  try {
    const account = await getAccount(connection, ata, "confirmed");
    rawBalance = account.amount;
  } catch (err: unknown) {
    if (
      err instanceof TokenAccountNotFoundError ||
      err instanceof TokenInvalidAccountOwnerError
    ) {
      // No ATA → balance is 0.
    } else {
      throw err;
    }
  }

  const uiBalance = fromRawAmount(rawBalance, mintInfo.decimals);
  const minRaw = toRawAmount(minBalance, mintInfo.decimals);
  const isOwner = rawBalance >= minRaw;

  return {
    isOwner,
    rawBalance,
    uiBalance,
    decimals: mintInfo.decimals,
    ata,
  };
}

/**
 * Batch-verify ownership for multiple wallets.
 *
 * Returns a map from wallet address (base58) to the check result.
 */
export async function verifyOwnershipBatch(
  connection: Connection,
  owners: PublicKey[],
  mint: PublicKey,
  minBalance: number = 1,
): Promise<Map<string, OwnershipCheckResult>> {
  const results = new Map<string, OwnershipCheckResult>();

  // Run checks concurrently in small batches to avoid overwhelming the RPC.
  const BATCH_SIZE = 20;
  for (let i = 0; i < owners.length; i += BATCH_SIZE) {
    const batch = owners.slice(i, i + BATCH_SIZE);
    const checks = batch.map((owner) =>
      verifyOwnership(connection, owner, mint, minBalance).then((result) => ({
        key: owner.toBase58(),
        result,
      })),
    );
    const settled = await Promise.allSettled(checks);
    for (const outcome of settled) {
      if (outcome.status === "fulfilled") {
        results.set(outcome.value.key, outcome.value.result);
      }
    }
  }

  return results;
}

// ---------------------------------------------------------------------------
// Query helpers
// ---------------------------------------------------------------------------

/**
 * Retrieve the SPL token balance for a wallet + mint.
 *
 * Returns `{ raw, ui, decimals }` or `null` if the wallet has no token
 * account for this mint.
 */
export async function getTokenBalance(
  connection: Connection,
  owner: PublicKey,
  mint: PublicKey,
): Promise<{ raw: bigint; ui: number; decimals: number } | null> {
  const ata = deriveATA(owner, mint);

  try {
    const account = await getAccount(connection, ata, "confirmed");
    const mintInfo = await fetchMint(connection, mint);
    return {
      raw: account.amount,
      ui: fromRawAmount(account.amount, mintInfo.decimals),
      decimals: mintInfo.decimals,
    };
  } catch (err: unknown) {
    if (
      err instanceof TokenAccountNotFoundError ||
      err instanceof TokenInvalidAccountOwnerError
    ) {
      return null;
    }
    throw err;
  }
}

/**
 * Get the total supply of an SPL token in human-readable form.
 */
export async function getTokenSupply(
  connection: Connection,
  mint: PublicKey,
): Promise<{ raw: bigint; ui: number; decimals: number }> {
  const mintInfo = await fetchMint(connection, mint);
  return {
    raw: mintInfo.supply,
    ui: fromRawAmount(mintInfo.supply, mintInfo.decimals),
    decimals: mintInfo.decimals,
  };
}

/**
 * Get SOL balance for a wallet (in SOL, not lamports).
 */
export async function getSolBalance(
  connection: Connection,
  pubkey: PublicKey,
): Promise<number> {
  const lamports = await connection.getBalance(pubkey, "confirmed");
  return lamports / LAMPORTS_PER_SOL;
}

// ---------------------------------------------------------------------------
// Largest token holders (useful for snapshot-holders)
// ---------------------------------------------------------------------------

/**
 * Single holder entry from `getTokenLargestAccounts`.
 */
export interface TokenHolder {
  /** The token account address. */
  tokenAccount: PublicKey;

  /** The owner of the token account (the wallet). */
  owner: PublicKey | null;

  /** Raw amount held. */
  rawAmount: bigint;

  /** Human-readable balance. */
  uiAmount: number;

  /** Token decimals. */
  decimals: number;
}

/**
 * Fetch the largest token holders for a given mint.
 *
 * The Solana RPC caps this at 20 results.  For a full holder snapshot,
 * use `getProgramAccounts` with appropriate filters (see
 * {@link snapshotAllHolders}).
 */
export async function getLargestHolders(
  connection: Connection,
  mint: PublicKey,
): Promise<TokenHolder[]> {
  const result = await connection.getTokenLargestAccounts(mint, "confirmed");
  const holders: TokenHolder[] = [];

  for (const entry of result.value) {
    let owner: PublicKey | null = null;
    try {
      const accountInfo = await getAccount(
        connection,
        entry.address,
        "confirmed",
      );
      owner = accountInfo.owner;
    } catch {
      // If we can't fetch the account, leave owner as null.
    }

    holders.push({
      tokenAccount: entry.address,
      owner,
      rawAmount: BigInt(entry.amount),
      uiAmount: entry.uiAmount ?? 0,
      decimals: entry.decimals,
    });
  }

  return holders;
}

/**
 * Snapshot all holders of a token by querying `getProgramAccounts` with
 * filters for the mint.
 *
 * This can be slow for tokens with many holders because it fetches all
 * token accounts from the Token Program.
 */
export async function snapshotAllHolders(
  connection: Connection,
  mint: PublicKey,
): Promise<
  Array<{
    tokenAccount: PublicKey;
    owner: PublicKey;
    rawBalance: bigint;
  }>
> {
  // SPL Token account layout:
  //   - bytes  0..31:  mint        (32 bytes)
  //   - bytes 32..63:  owner       (32 bytes)
  //   - bytes 64..71:  amount      (u64 LE, 8 bytes)
  //   ...
  // Total size = 165 bytes.
  const accounts = await connection.getProgramAccounts(TOKEN_PROGRAM_ID, {
    commitment: "confirmed",
    filters: [
      { dataSize: 165 }, // SPL Token Account size
      {
        memcmp: {
          offset: 0,
          bytes: mint.toBase58(),
        },
      },
    ],
  });

  const holders: Array<{
    tokenAccount: PublicKey;
    owner: PublicKey;
    rawBalance: bigint;
  }> = [];

  for (const { pubkey, account } of accounts) {
    const data = account.data;

    // Owner is bytes 32..63
    const ownerBytes = data.subarray(32, 64);
    const owner = new PublicKey(ownerBytes);

    // Amount is bytes 64..71 (u64 LE)
    const amountBytes = data.subarray(64, 72);
    const rawBalance = amountBytes.reduce(
      (acc: bigint, byte: number, i: number) =>
        acc + BigInt(byte) * (BigInt(1) << BigInt(i * 8)),
      BigInt(0),
    );

    // Skip zero-balance accounts
    if (rawBalance > BigInt(0)) {
      holders.push({ tokenAccount: pubkey, owner, rawBalance });
    }
  }

  // Sort descending by balance.
  holders.sort((a, b) => {
    if (a.rawBalance > b.rawBalance) return -1;
    if (a.rawBalance < b.rawBalance) return 1;
    return 0;
  });

  return holders;
}

// ---------------------------------------------------------------------------
// Airdrop helper (devnet / testnet only)
// ---------------------------------------------------------------------------

/**
 * Request an airdrop of SOL to `recipient` on devnet/testnet.
 *
 * @param connection - Solana RPC connection (must point to devnet/testnet).
 * @param recipient  - The wallet to fund.
 * @param solAmount  - Amount of SOL to request (default 1).
 */
export async function requestAirdrop(
  connection: Connection,
  recipient: PublicKey,
  solAmount: number = 1,
): Promise<string> {
  const lamports = Math.round(solAmount * LAMPORTS_PER_SOL);
  const signature = await connection.requestAirdrop(recipient, lamports);
  await connection.confirmTransaction(signature, "confirmed");
  return signature;
}
