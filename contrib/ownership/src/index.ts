/**
 * OpenTela Ownership Tools – Public API
 *
 * Barrel export that re-exports all public functions, types, and
 * constants from the ownership toolkit.  Consumers can import from
 * this single entry-point instead of reaching into individual modules:
 *
 *   import { loadConfig, createSPLToken, verifyOwnership } from "@opentela/ownership-tools";
 */

// ── Configuration ──────────────────────────────────────────────────────────

export {
  loadConfig,
  resetConfig,
  getConnection,
  printConfig,
  loadKeypairFromFile,
  loadKeypairFromBase58,
  type OwnershipConfig,
} from "./config.js";

// ── Logging Utilities ──────────────────────────────────────────────────────

export {
  info,
  success,
  warn,
  error,
  debug,
  kv,
  header,
  divider,
  blank,
  step,
  explorerTxUrl,
  explorerAddressUrl,
  logTx,
  fatal,
  setDebug,
  isDebug,
} from "./log.js";

// ── SPL Token Helpers ──────────────────────────────────────────────────────

export {
  // ATA helpers
  deriveATA,
  getOrCreateATA,
  buildCreateATAInstruction,
  ataExists,

  // Mint helpers
  fetchMint,
  mintExists,
  createSPLToken,
  ensureMint,
  type CreateTokenResult,

  // Minting
  mintTokensTo,
  toRawAmount,
  fromRawAmount,

  // Transfer
  transferTokens,

  // Burn
  burnTokens,

  // Authority management
  revokeMintAuthority,
  revokeFreezeAuthority,
  transferMintAuthority,

  // Ownership verification
  verifyOwnership,
  verifyOwnershipBatch,
  type OwnershipCheckResult,

  // Query helpers
  getTokenBalance,
  getTokenSupply,
  getSolBalance,

  // Holder snapshots
  getLargestHolders,
  snapshotAllHolders,
  type TokenHolder,

  // Devnet / testnet utility
  requestAirdrop,
} from "./spl-helpers.js";
