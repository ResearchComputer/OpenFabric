/**
 * OpenTela Ownership Tools – Token Info
 *
 * Displays detailed information about an SPL token including supply,
 * authorities (mint / freeze), on-chain metadata, and optionally the
 * largest token holders.
 *
 * Usage:
 *   npx tsx src/token-info.ts [options]
 *
 * Options:
 *   --mint <pubkey>     Token mint address (overrides TOKEN_MINT from .env).
 *   --holders           Also display the largest token holders (top 20).
 *   --all-holders       Snapshot *all* holders via getProgramAccounts
 *                        (can be slow for widely-held tokens).
 *   --json              Output as JSON instead of human-readable text.
 *
 * Environment / .env:
 *   TOKEN_MINT, SOLANA_CLUSTER, KEYPAIR_PATH, etc.
 */

import { Command } from "commander";
import { PublicKey, Connection } from "@solana/web3.js";
import { loadConfig, getConnection, printConfig } from "./config.js";
import {
  fetchMint,
  mintExists,
  fromRawAmount,
  getTokenSupply,
  getLargestHolders,
  snapshotAllHolders,
  type TokenHolder,
} from "./spl-helpers.js";
import * as log from "./log.js";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface TokenInfoResult {
  mint: string;
  decimals: number;
  supply: {
    raw: string;
    ui: number;
  };
  mintAuthority: string | null;
  freezeAuthority: string | null;
  isInitialized: boolean;
  metadata: OnChainMetadata | null;
  holders?: HolderEntry[];
}

interface OnChainMetadata {
  name: string;
  symbol: string;
  uri: string;
  sellerFeeBasisPoints: number;
  updateAuthority: string;
  isMutable: boolean;
}

interface HolderEntry {
  rank: number;
  owner: string | null;
  tokenAccount: string;
  rawBalance: string;
  uiBalance: number;
}

// ---------------------------------------------------------------------------
// Metadata PDA derivation & fetching
// ---------------------------------------------------------------------------

const TOKEN_METADATA_PROGRAM_ID = new PublicKey(
  "metaqbxxUerdq28cj1RbAWkYQm3ybzjb6a8bt518x1s",
);

function deriveMetadataPDA(mint: PublicKey): PublicKey {
  const [pda] = PublicKey.findProgramAddressSync(
    [
      Buffer.from("metadata"),
      TOKEN_METADATA_PROGRAM_ID.toBuffer(),
      mint.toBuffer(),
    ],
    TOKEN_METADATA_PROGRAM_ID,
  );
  return pda;
}

/**
 * Attempt to fetch and decode on-chain Metaplex Token Metadata.
 *
 * The metadata account uses a known layout.  We do a best-effort
 * parse — if the account does not exist or is unparseable we return
 * null rather than throwing.
 *
 * Layout (simplified, Borsh-encoded):
 *   - 1  byte:   key (enum discriminator, = 4 for MetadataV1)
 *   - 32 bytes:  update authority
 *   - 32 bytes:  mint
 *   -  4 bytes:  name string length (u32 LE) + name bytes
 *   -  4 bytes:  symbol string length (u32 LE) + symbol bytes
 *   -  4 bytes:  uri string length (u32 LE) + uri bytes
 *   -  2 bytes:  seller_fee_basis_points (u16 LE)
 *   ... (creators, collection, uses, etc. follow but are optional)
 *
 * We also look for the `isMutable` flag which sits after some optional
 * sections.  For simplicity we use a regex-free linear scan.
 */
async function fetchOnChainMetadata(
  connection: Connection,
  mint: PublicKey,
): Promise<OnChainMetadata | null> {
  const metadataPDA = deriveMetadataPDA(mint);

  const accountInfo = await connection.getAccountInfo(metadataPDA, "confirmed");
  if (!accountInfo || !accountInfo.data || accountInfo.data.length < 70) {
    return null;
  }

  try {
    const data = accountInfo.data;
    let offset = 0;

    // Key discriminator (1 byte) — 4 = MetadataV1
    const key = data[offset];
    offset += 1;
    if (key !== 4) {
      log.debug(`Metadata account key is ${key}, expected 4 (MetadataV1)`);
      return null;
    }

    // Update authority (32 bytes)
    const updateAuthority = new PublicKey(data.subarray(offset, offset + 32));
    offset += 32;

    // Mint (32 bytes) — should match the mint we queried
    offset += 32;

    // Name (4-byte length prefix + UTF-8 bytes)
    const nameLen = data.readUInt32LE(offset);
    offset += 4;
    const name = data
      .subarray(offset, offset + nameLen)
      .toString("utf-8")
      .replace(/\0+$/, "");
    offset += nameLen;

    // Symbol (4-byte length prefix + UTF-8 bytes)
    const symbolLen = data.readUInt32LE(offset);
    offset += 4;
    const symbol = data
      .subarray(offset, offset + symbolLen)
      .toString("utf-8")
      .replace(/\0+$/, "");
    offset += symbolLen;

    // URI (4-byte length prefix + UTF-8 bytes)
    const uriLen = data.readUInt32LE(offset);
    offset += 4;
    const uri = data
      .subarray(offset, offset + uriLen)
      .toString("utf-8")
      .replace(/\0+$/, "");
    offset += uriLen;

    // Seller fee basis points (u16 LE)
    const sellerFeeBasisPoints = data.readUInt16LE(offset);
    offset += 2;

    // Creators optional (1 byte option flag).  Skip over if present so we
    // can reach isMutable.
    const hasCreators = data[offset] === 1;
    offset += 1;
    if (hasCreators) {
      // 4-byte creator count
      const numCreators = data.readUInt32LE(offset);
      offset += 4;
      // Each creator: 32 bytes address + 1 byte verified + 1 byte share
      offset += numCreators * 34;
    }

    // Collection optional
    const hasCollection = data[offset] === 1;
    offset += 1;
    if (hasCollection) {
      // 1 byte verified + 32 bytes key
      offset += 33;
    }

    // Uses optional
    const hasUses = data[offset] === 1;
    offset += 1;
    if (hasUses) {
      // useMethod (1 byte) + remaining (u64) + total (u64)
      offset += 17;
    }

    // isMutable (1 byte)
    let isMutable = true;
    if (offset < data.length) {
      isMutable = data[offset] === 1;
    }

    return {
      name,
      symbol,
      uri,
      sellerFeeBasisPoints,
      updateAuthority: updateAuthority.toBase58(),
      isMutable,
    };
  } catch (err) {
    log.debug("Failed to parse metadata account:", err);
    return null;
  }
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function parsePubkey(value: string, label: string): PublicKey {
  try {
    return new PublicKey(value);
  } catch {
    log.fatal(`Invalid ${label} public key: "${value}"`);
  }
}

// ---------------------------------------------------------------------------
// Display helpers
// ---------------------------------------------------------------------------

function printTokenInfo(info: TokenInfoResult, cluster: string): void {
  log.header("Token Information");

  log.kv("Mint Address", info.mint);
  log.kv("Explorer", log.explorerAddressUrl(info.mint, cluster));
  log.kv("Initialized", info.isInitialized ? "yes" : "no");
  log.kv("Decimals", info.decimals);
  log.blank();

  log.kv("Total Supply (raw)", info.supply.raw);
  log.kv("Total Supply (UI)", info.supply.ui.toString());
  log.blank();

  log.kv("Mint Authority", info.mintAuthority ?? "(disabled – minting locked)");
  log.kv(
    "Freeze Authority",
    info.freezeAuthority ?? "(disabled – freezing locked)",
  );
  log.blank();

  // ── Metadata ─────────────────────────────────────────────────────────

  if (info.metadata) {
    log.header("On-Chain Metadata (Metaplex Token Metadata)");
    log.kv("Name", info.metadata.name || "(empty)");
    log.kv("Symbol", info.metadata.symbol || "(empty)");
    log.kv("URI", info.metadata.uri || "(empty)");
    log.kv("Seller Fee (bps)", info.metadata.sellerFeeBasisPoints);
    log.kv("Update Authority", info.metadata.updateAuthority);
    log.kv("Mutable", info.metadata.isMutable ? "yes" : "no");
    log.blank();
  } else {
    log.info(
      "No on-chain Metaplex metadata found for this mint.\n" +
        "  You can attach metadata with: npx tsx src/create-token.ts",
    );
    log.blank();
  }

  // ── Holders ──────────────────────────────────────────────────────────

  if (info.holders && info.holders.length > 0) {
    log.header("Token Holders");

    const rankWidth = 4;
    const balanceWidth = 24;

    // Table header
    const hdr =
      "Rank".padEnd(rankWidth + 2) +
      "Owner".padEnd(46) +
      "Balance".padStart(balanceWidth);
    console.log(`  ${hdr}`);
    console.log(`  ${"─".repeat(hdr.length)}`);

    for (const h of info.holders) {
      const rank = `#${h.rank}`.padEnd(rankWidth + 2);
      const owner = (h.owner ?? "(unknown)").padEnd(46);
      const balance = `${h.uiBalance}`.padStart(balanceWidth);
      console.log(`  ${rank}${owner}${balance}`);
    }
    log.blank();
    log.info(`Showing ${info.holders.length} holder(s).`);
    log.blank();
  }
}

function printTokenInfoJSON(info: TokenInfoResult): void {
  console.log(JSON.stringify(info, null, 2));
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function main(): Promise<void> {
  const program = new Command()
    .name("token-info")
    .description("Display detailed information about an OpenTela SPL token")
    .option("--mint <pubkey>", "Token mint address (overrides TOKEN_MINT)")
    .option("--holders", "Also display the largest token holders (top 20)")
    .option(
      "--all-holders",
      "Snapshot all holders via getProgramAccounts (can be slow)",
    )
    .option("--json", "Output as JSON instead of human-readable text")
    .parse(process.argv);

  const opts = program.opts();

  // ── Configuration ────────────────────────────────────────────────────

  const cfg = loadConfig();
  if (cfg.debug) log.setDebug(true);

  const connection = getConnection(cfg);
  const jsonOutput = opts.json === true;
  const showHolders = opts.holders === true;
  const showAllHolders = opts.allHolders === true;

  // Resolve mint address.
  const mintAddress = (opts.mint as string | undefined) ?? cfg.tokenMint;
  if (!mintAddress) {
    log.fatal(
      "No token mint specified.\n" +
        "  Set TOKEN_MINT in .env or pass --mint <pubkey>.\n" +
        "  Run `npx tsx src/create-token.ts` to create one first.",
    );
  }
  const mint = parsePubkey(mintAddress, "mint");

  // ── Print header (only in human-readable mode) ───────────────────────

  if (!jsonOutput) {
    log.header("OpenTela Token Info");
    printConfig(cfg);
    log.blank();
    log.step(
      1,
      showHolders || showAllHolders ? 3 : 2,
      "Fetching mint account…",
    );
  }

  // ── Fetch mint ───────────────────────────────────────────────────────

  const exists = await mintExists(connection, mint);
  if (!exists) {
    log.fatal(
      `Mint ${mint.toBase58()} does not exist on-chain.\n` +
        "  Check the TOKEN_MINT value in .env or pass --mint with a valid address.",
    );
  }

  const mintInfo = await fetchMint(connection, mint);
  const supply = await getTokenSupply(connection, mint);

  // ── Fetch metadata ───────────────────────────────────────────────────

  if (!jsonOutput) {
    log.step(
      2,
      showHolders || showAllHolders ? 3 : 2,
      "Fetching on-chain metadata…",
    );
  }

  const metadata = await fetchOnChainMetadata(connection, mint);

  // ── Fetch holders (optional) ─────────────────────────────────────────

  let holders: HolderEntry[] | undefined;

  if (showAllHolders) {
    if (!jsonOutput) {
      log.step(
        3,
        3,
        "Snapshotting all token holders (this may take a moment)…",
      );
    }

    const allHolders = await snapshotAllHolders(connection, mint);
    holders = allHolders.map((h, idx) => ({
      rank: idx + 1,
      owner: h.owner.toBase58(),
      tokenAccount: h.tokenAccount.toBase58(),
      rawBalance: h.rawBalance.toString(),
      uiBalance: fromRawAmount(h.rawBalance, mintInfo.decimals),
    }));
  } else if (showHolders) {
    if (!jsonOutput) {
      log.step(3, 3, "Fetching largest token holders…");
    }

    const topHolders = await getLargestHolders(connection, mint);
    holders = topHolders.map((h: TokenHolder, idx: number) => ({
      rank: idx + 1,
      owner: h.owner?.toBase58() ?? null,
      tokenAccount: h.tokenAccount.toBase58(),
      rawBalance: h.rawAmount.toString(),
      uiBalance: h.uiAmount,
    }));
  }

  // ── Build result ─────────────────────────────────────────────────────

  const result: TokenInfoResult = {
    mint: mint.toBase58(),
    decimals: mintInfo.decimals,
    supply: {
      raw: supply.raw.toString(),
      ui: supply.ui,
    },
    mintAuthority: mintInfo.mintAuthority?.toBase58() ?? null,
    freezeAuthority: mintInfo.freezeAuthority?.toBase58() ?? null,
    isInitialized: mintInfo.isInitialized,
    metadata: metadata
      ? {
          name: metadata.name,
          symbol: metadata.symbol,
          uri: metadata.uri,
          sellerFeeBasisPoints: metadata.sellerFeeBasisPoints,
          updateAuthority: metadata.updateAuthority,
          isMutable: metadata.isMutable,
        }
      : null,
    holders,
  };

  // ── Output ───────────────────────────────────────────────────────────

  if (jsonOutput) {
    printTokenInfoJSON(result);
  } else {
    log.blank();
    printTokenInfo(result, cfg.cluster);

    log.divider();
    log.success("Token info retrieved successfully.");
    log.blank();
    log.info("Related commands:");
    log.kv("Mint tokens", "npx tsx src/mint-tokens.ts --amount <n>");
    log.kv(
      "Transfer",
      "npx tsx src/transfer-tokens.ts --to <wallet> --amount <n>",
    );
    log.kv(
      "Verify ownership",
      "npx tsx src/verify-ownership.ts --wallet <pubkey>",
    );
    log.kv("Revoke minting", "npx tsx src/revoke-mint.ts   (⚠ irreversible)");
    log.kv("Burn tokens", "npx tsx src/burn-tokens.ts --amount <n>");
    log.blank();
  }
}

main().catch((err) => {
  log.error("Failed to retrieve token info:", err);
  process.exit(1);
});
