/**
 * OpenTela Ownership Tools – Shared Configuration
 *
 * Reads environment variables (with .env support) and provides typed
 * accessors for the Solana cluster, keypair, token mint, and other
 * settings used across all ownership scripts.
 */

import { Keypair, Connection, clusterApiUrl, Cluster } from "@solana/web3.js";
import { readFileSync, existsSync, readdirSync, statSync } from "node:fs";
import { resolve } from "node:path";
import { homedir } from "node:os";
import dotenv from "dotenv";
import bs58 from "bs58";

// ---------------------------------------------------------------------------
// Load .env (idempotent – safe to import this module from multiple files)
// ---------------------------------------------------------------------------

dotenv.config({ path: resolve(import.meta.dirname ?? ".", "../.env") });

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface OwnershipConfig {
  /** Resolved Solana JSON-RPC URL. */
  rpcUrl: string;

  /** Cluster label (mainnet-beta | devnet | testnet | localnet | custom). */
  cluster: string;

  /** The fee-payer / authority keypair loaded from disk or env var. */
  keypair: Keypair;

  /** Token mint address (may be empty when creating a new token). */
  tokenMint: string;

  /** Human-readable token name (for create-token metadata). */
  tokenName: string;

  /** Token ticker symbol. */
  tokenSymbol: string;

  /** Number of decimal places for the token. */
  tokenDecimals: number;

  /** Off-chain metadata URI (e.g. Arweave / IPFS link). */
  tokenUri: string;

  /** Initial supply to mint right after token creation (whole tokens). */
  tokenInitialSupply: number;

  /** Anchor program ID (optional, only for Anchor-based minting). */
  anchorProgramId: string;

  /** Minimum token balance for ownership verification. */
  ownershipMinBalance: number;

  /** Whether debug / verbose logging is enabled. */
  debug: boolean;
}

// ---------------------------------------------------------------------------
// Cluster resolution
// ---------------------------------------------------------------------------

const KNOWN_CLUSTERS: Record<string, string> = {
  "mainnet-beta": "https://api.mainnet-beta.solana.com",
  mainnet: "https://api.mainnet-beta.solana.com",
  devnet: "https://api.devnet.solana.com",
  testnet: "https://api.testnet.solana.com",
  localnet: "http://127.0.0.1:8899",
  localhost: "http://127.0.0.1:8899",
};

function resolveRpcUrl(): { rpcUrl: string; cluster: string } {
  // Explicit URL always wins.
  const explicitUrl = process.env.SOLANA_RPC_URL?.trim();
  if (explicitUrl) {
    return { rpcUrl: explicitUrl, cluster: "custom" };
  }

  const label = (process.env.SOLANA_CLUSTER ?? "devnet").trim().toLowerCase();
  const known = KNOWN_CLUSTERS[label];
  if (known) {
    return { rpcUrl: known, cluster: label };
  }

  // If the label looks like a URL, use it directly.
  if (label.startsWith("http://") || label.startsWith("https://")) {
    return { rpcUrl: label, cluster: "custom" };
  }

  // Last resort: try clusterApiUrl (will throw if invalid).
  try {
    return { rpcUrl: clusterApiUrl(label as Cluster), cluster: label };
  } catch {
    throw new Error(
      `Unknown Solana cluster "${label}". ` +
        `Set SOLANA_CLUSTER to one of: ${Object.keys(KNOWN_CLUSTERS).join(", ")} ` +
        `or provide a full URL via SOLANA_RPC_URL.`,
    );
  }
}

// ---------------------------------------------------------------------------
// Keypair loading
// ---------------------------------------------------------------------------

/**
 * Default keypair search paths, checked in order.
 */
const DEFAULT_KEYPAIR_PATHS = [
  resolve(homedir(), ".config/opentela/accounts"),
  resolve(homedir(), ".config/solana/id.json"),
];

/**
 * Parse a Solana CLI keypair JSON file (array of 64 uint8 values).
 */
function loadKeypairFromFile(filePath: string): Keypair {
  const absPath = resolve(filePath);
  if (!existsSync(absPath)) {
    throw new Error(`Keypair file not found: ${absPath}`);
  }
  const raw = readFileSync(absPath, "utf-8");
  const parsed: number[] = JSON.parse(raw);

  if (!Array.isArray(parsed) || parsed.length !== 64) {
    throw new Error(
      `Invalid keypair file ${absPath}: expected a JSON array of 64 integers, ` +
        `got ${Array.isArray(parsed) ? `array of length ${parsed.length}` : typeof parsed}.`,
    );
  }

  return Keypair.fromSecretKey(Uint8Array.from(parsed));
}

/**
 * Decode a base58-encoded private key (the Phantom export format —
 * 64 bytes: 32-byte secret scalar ‖ 32-byte public key).
 */
function loadKeypairFromBase58(encoded: string): Keypair {
  const decoded = bs58.decode(encoded.trim());
  if (decoded.length !== 64) {
    throw new Error(
      `Invalid base58 private key: expected 64 bytes, got ${decoded.length}.`,
    );
  }
  return Keypair.fromSecretKey(decoded);
}

/**
 * Attempt to find the first OpenTela-managed keypair inside the accounts
 * directory (~/.config/opentela/accounts/<pubkey>/keypair.json).
 */
function findFirstOpenTelaKeypair(accountsDir: string): Keypair | null {
  if (!existsSync(accountsDir)) return null;

  // The directory structure is: accounts/<base58-pubkey>/keypair.json
  let entries: string[];
  try {
    entries = readdirSync(accountsDir);
  } catch {
    return null;
  }

  for (const entry of entries.sort()) {
    const kpPath = resolve(accountsDir, entry, "keypair.json");
    try {
      if (statSync(kpPath).isFile()) {
        return loadKeypairFromFile(kpPath);
      }
    } catch {
      // skip entries without a keypair
    }
  }
  return null;
}

function resolveKeypair(): Keypair {
  // 1. Explicit file path from env.
  const envPath = process.env.KEYPAIR_PATH?.trim();
  if (envPath) {
    return loadKeypairFromFile(envPath);
  }

  // 2. Base58-encoded private key from env.
  const envKey = process.env.PRIVATE_KEY_BASE58?.trim();
  if (envKey) {
    return loadKeypairFromBase58(envKey);
  }

  // 3. Auto-discover from well-known locations.
  for (const candidate of DEFAULT_KEYPAIR_PATHS) {
    try {
      // If it's a directory, look for the first managed keypair inside it.
      if (statSync(candidate).isDirectory()) {
        const kp = findFirstOpenTelaKeypair(candidate);
        if (kp) return kp;
        continue;
      }
    } catch {
      // not a directory – try as a file
    }

    try {
      return loadKeypairFromFile(candidate);
    } catch {
      // try next candidate
    }
  }

  throw new Error(
    "No keypair found. Provide one via:\n" +
      "  • KEYPAIR_PATH=<path-to-keypair.json> in .env\n" +
      "  • PRIVATE_KEY_BASE58=<base58-key> in .env\n" +
      "  • Place a Solana CLI keypair at ~/.config/solana/id.json\n" +
      "  • Create an OpenTela wallet: otela wallet create",
  );
}

// ---------------------------------------------------------------------------
// Env helpers
// ---------------------------------------------------------------------------

function envString(key: string, fallback: string): string {
  return process.env[key]?.trim() || fallback;
}

function envInt(key: string, fallback: number): number {
  const raw = process.env[key]?.trim();
  if (!raw) return fallback;
  const n = parseInt(raw, 10);
  if (Number.isNaN(n)) {
    throw new Error(`Invalid integer for ${key}: "${raw}"`);
  }
  return n;
}

function envFloat(key: string, fallback: number): number {
  const raw = process.env[key]?.trim();
  if (!raw) return fallback;
  const n = parseFloat(raw);
  if (Number.isNaN(n)) {
    throw new Error(`Invalid number for ${key}: "${raw}"`);
  }
  return n;
}

function envBool(key: string, fallback: boolean): boolean {
  const raw = process.env[key]?.trim().toLowerCase();
  if (!raw) return fallback;
  return raw === "true" || raw === "1" || raw === "yes";
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

let _cached: OwnershipConfig | null = null;

/**
 * Load and return the fully-resolved configuration.
 *
 * The result is cached after the first call so that repeated imports
 * don't re-read the filesystem.
 */
export function loadConfig(): OwnershipConfig {
  if (_cached) return _cached;

  const { rpcUrl, cluster } = resolveRpcUrl();
  const keypair = resolveKeypair();

  _cached = {
    rpcUrl,
    cluster,
    keypair,
    tokenMint: envString("TOKEN_MINT", ""),
    tokenName: envString("TOKEN_NAME", "OpenTela"),
    tokenSymbol: envString("TOKEN_SYMBOL", "OTELA"),
    tokenDecimals: envInt("TOKEN_DECIMALS", 9),
    tokenUri: envString("TOKEN_URI", ""),
    tokenInitialSupply: envFloat("TOKEN_INITIAL_SUPPLY", 0),
    anchorProgramId: envString("ANCHOR_PROGRAM_ID", ""),
    ownershipMinBalance: envFloat("OWNERSHIP_MIN_BALANCE", 1),
    debug: envBool("DEBUG", false),
  };

  return _cached;
}

/**
 * Reset the cached config (useful in tests).
 */
export function resetConfig(): void {
  _cached = null;
}

/**
 * Create a Solana Connection from the resolved config.
 */
export function getConnection(cfg?: OwnershipConfig): Connection {
  const config = cfg ?? loadConfig();
  return new Connection(config.rpcUrl, "confirmed");
}

/**
 * Pretty-print the current configuration (masking sensitive values).
 */
export function printConfig(cfg?: OwnershipConfig): void {
  const config = cfg ?? loadConfig();
  const pubkey = config.keypair.publicKey.toBase58();
  console.log("┌─────────────────────────────────────────────────────────");
  console.log("│ OpenTela Ownership Tools – Configuration");
  console.log("├─────────────────────────────────────────────────────────");
  console.log(`│ Cluster:        ${config.cluster}`);
  console.log(`│ RPC URL:        ${config.rpcUrl}`);
  console.log(`│ Authority:      ${pubkey}`);
  console.log(
    `│ Token Mint:     ${config.tokenMint || "(not set – will be created)"}`,
  );
  console.log(`│ Token Name:     ${config.tokenName}`);
  console.log(`│ Token Symbol:   ${config.tokenSymbol}`);
  console.log(`│ Token Decimals: ${config.tokenDecimals}`);
  console.log(`│ Min Balance:    ${config.ownershipMinBalance}`);
  console.log(`│ Debug:          ${config.debug}`);
  console.log("└─────────────────────────────────────────────────────────");
}

// Re-export helpers that other scripts may need.
export { loadKeypairFromFile, loadKeypairFromBase58 };
