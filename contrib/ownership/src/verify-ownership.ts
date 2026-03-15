/**
 * OpenTela Ownership Tools – Verify Ownership
 *
 * Checks whether one or more Solana wallets hold the required minimum
 * balance of the OpenTela SPL ownership token.  This is the primary
 * mechanism for proving that a node operator is a legitimate provider
 * in the OpenTela network.
 *
 * Usage:
 *   npx tsx src/verify-ownership.ts --wallet <pubkey>
 *   npx tsx src/verify-ownership.ts --wallet <pubkey1> --wallet <pubkey2>
 *   npx tsx src/verify-ownership.ts --file wallets.txt
 *   echo "<pubkey>" | npx tsx src/verify-ownership.ts --stdin
 *
 * Options:
 *   --wallet <pubkey>   Wallet public key(s) to verify.  Can be specified
 *                        multiple times.
 *   --file <path>       Path to a newline-delimited file of wallet public
 *                        keys to verify in batch.
 *   --stdin             Read wallet public keys from stdin (one per line).
 *   --mint <pubkey>     Token mint address (overrides TOKEN_MINT from .env).
 *   --min-balance <n>   Minimum token balance required (whole tokens).
 *                        Overrides OWNERSHIP_MIN_BALANCE from .env.
 *   --json              Output results as JSON instead of human-readable text.
 *   --quiet             Only output the final pass/fail status (exit code
 *                        reflects the result: 0 = all pass, 1 = any fail).
 *   --verbose           Show additional details for each wallet checked.
 *
 * Environment / .env:
 *   TOKEN_MINT, OWNERSHIP_MIN_BALANCE, SOLANA_CLUSTER, KEYPAIR_PATH, etc.
 *
 * Exit codes:
 *   0 – All checked wallets meet the ownership requirement.
 *   1 – One or more wallets failed the ownership check (or an error occurred).
 */

import { Command } from "commander";
import { PublicKey } from "@solana/web3.js";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { loadConfig, getConnection, printConfig } from "./config.js";
import {
  verifyOwnership,
  verifyOwnershipBatch,
  fromRawAmount,
  mintExists,
  fetchMint,
} from "./spl-helpers.js";
import * as log from "./log.js";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface VerificationEntry {
  wallet: string;
  isOwner: boolean;
  rawBalance: string;
  uiBalance: number;
  requiredBalance: number;
  ata: string;
  decimals: number;
  error?: string;
}

interface VerificationReport {
  mint: string;
  minBalance: number;
  cluster: string;
  timestamp: string;
  totalChecked: number;
  totalPassed: number;
  totalFailed: number;
  results: VerificationEntry[];
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

/**
 * Collect wallet addresses from all input sources (--wallet, --file, --stdin).
 */
function collectWallets(opts: Record<string, unknown>): string[] {
  const wallets: string[] = [];

  // --wallet (may be a single string or an array when specified multiple times)
  const walletOpt = opts.wallet;
  if (walletOpt) {
    if (Array.isArray(walletOpt)) {
      wallets.push(...(walletOpt as string[]));
    } else {
      wallets.push(walletOpt as string);
    }
  }

  // --file
  const filePath = opts.file as string | undefined;
  if (filePath) {
    try {
      const absPath = resolve(filePath);
      const content = readFileSync(absPath, "utf-8");
      const lines = content
        .split(/\r?\n/)
        .map((line) => line.trim())
        .filter((line) => line.length > 0 && !line.startsWith("#"));
      wallets.push(...lines);
    } catch (err) {
      log.fatal(`Failed to read wallet file "${filePath}": ${err}`);
    }
  }

  // --stdin
  if (opts.stdin === true) {
    try {
      // Read synchronously from stdin (fd 0).
      const stdinContent = readFileSync(0, "utf-8");
      const lines = stdinContent
        .split(/\r?\n/)
        .map((line) => line.trim())
        .filter((line) => line.length > 0 && !line.startsWith("#"));
      wallets.push(...lines);
    } catch {
      // stdin may not be available in all environments; silently skip.
    }
  }

  // Deduplicate while preserving order.
  const seen = new Set<string>();
  const unique: string[] = [];
  for (const w of wallets) {
    if (!seen.has(w)) {
      seen.add(w);
      unique.push(w);
    }
  }

  return unique;
}

// ---------------------------------------------------------------------------
// Display helpers
// ---------------------------------------------------------------------------

const PASS_ICON = "✔";
const FAIL_ICON = "✖";

function printSingleResult(
  entry: VerificationEntry,
  cluster: string,
  verbose: boolean,
): void {
  const statusIcon = entry.isOwner
    ? log.explorerAddressUrl("", "") && PASS_ICON
    : FAIL_ICON;
  const statusLabel = entry.isOwner ? "PASS" : "FAIL";
  const statusColor = entry.isOwner
    ? `\x1b[32m${statusIcon} ${statusLabel}\x1b[0m`
    : `\x1b[31m${statusIcon} ${statusLabel}\x1b[0m`;

  if (entry.error) {
    console.log(`  ${statusColor}  ${entry.wallet}  (error: ${entry.error})`);
    return;
  }

  console.log(
    `  ${statusColor}  ${entry.wallet}  ` +
      `balance: ${entry.uiBalance} (required: ≥${entry.requiredBalance})`,
  );

  if (verbose) {
    log.kv("ATA", entry.ata, 10);
    log.kv("Raw Balance", entry.rawBalance, 10);
    log.kv("Decimals", entry.decimals, 10);
    log.kv("Explorer", log.explorerAddressUrl(entry.wallet, cluster), 10);
    log.blank();
  }
}

function printReport(report: VerificationReport, verbose: boolean): void {
  log.header("Ownership Verification Report");

  log.kv("Mint", report.mint);
  log.kv("Cluster", report.cluster);
  log.kv("Min Balance", `${report.minBalance} token(s)`);
  log.kv("Timestamp", report.timestamp);
  log.blank();

  log.kv("Total Checked", report.totalChecked);
  log.kv("Passed", report.totalPassed);
  log.kv("Failed", report.totalFailed);
  log.blank();

  if (report.results.length > 0) {
    log.divider();
    log.blank();

    for (const entry of report.results) {
      printSingleResult(entry, report.cluster, verbose);
    }

    log.blank();
  }

  log.divider();

  if (report.totalFailed === 0) {
    log.success(
      `All ${report.totalChecked} wallet(s) meet the ownership requirement.`,
    );
  } else {
    log.error(
      `${report.totalFailed} of ${report.totalChecked} wallet(s) ` +
        `failed the ownership check.`,
    );
  }

  log.blank();
}

function printReportJSON(report: VerificationReport): void {
  console.log(JSON.stringify(report, null, 2));
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function main(): Promise<void> {
  const program = new Command()
    .name("verify-ownership")
    .description(
      "Verify that wallet(s) hold the required OpenTela ownership token balance",
    )
    .option(
      "--wallet <pubkey...>",
      "Wallet public key(s) to verify (repeatable)",
    )
    .option("--file <path>", "Newline-delimited file of wallet public keys")
    .option("--stdin", "Read wallet public keys from stdin")
    .option("--mint <pubkey>", "Token mint address (overrides TOKEN_MINT)")
    .option(
      "--min-balance <n>",
      "Minimum token balance required (whole tokens)",
      parseFloat,
    )
    .option("--json", "Output results as JSON")
    .option("--quiet", "Only output final pass/fail status")
    .option("--verbose", "Show additional details for each wallet")
    .parse(process.argv);

  const opts = program.opts();

  // ── Configuration ────────────────────────────────────────────────────

  const cfg = loadConfig();
  if (cfg.debug) log.setDebug(true);

  const connection = getConnection(cfg);
  const jsonOutput = opts.json === true;
  const quiet = opts.quiet === true;
  const verbose = opts.verbose === true;

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

  // Resolve minimum balance.
  const minBalance =
    (opts.minBalance as number | undefined) ?? cfg.ownershipMinBalance;

  // ── Collect wallets ──────────────────────────────────────────────────

  const walletStrings = collectWallets(opts as Record<string, unknown>);

  if (walletStrings.length === 0) {
    // If no wallets were provided via flags, default to the authority wallet.
    const defaultWallet = cfg.keypair.publicKey.toBase58();
    if (!quiet && !jsonOutput) {
      log.info(
        `No wallets specified – checking the authority wallet: ${defaultWallet}`,
      );
      log.blank();
    }
    walletStrings.push(defaultWallet);
  }

  // Validate all wallet addresses up front.
  const walletPubkeys: PublicKey[] = [];
  for (const ws of walletStrings) {
    try {
      walletPubkeys.push(new PublicKey(ws));
    } catch {
      if (!jsonOutput) {
        log.warn(`Skipping invalid public key: "${ws}"`);
      }
    }
  }

  if (walletPubkeys.length === 0) {
    log.fatal("No valid wallet addresses to check.");
  }

  // ── Print header ─────────────────────────────────────────────────────

  if (!jsonOutput && !quiet) {
    log.header("OpenTela Ownership Verification");
    printConfig(cfg);
    log.blank();
    log.kv("Mint", mint.toBase58());
    log.kv("Min Balance", `${minBalance} token(s)`);
    log.kv("Wallets to check", walletPubkeys.length);
    log.blank();
  }

  // ── Validate mint exists ─────────────────────────────────────────────

  if (!quiet && !jsonOutput) {
    log.step(1, 3, "Validating mint account…");
  }

  const exists = await mintExists(connection, mint);
  if (!exists) {
    log.fatal(
      `Mint ${mint.toBase58()} does not exist on-chain.\n` +
        "  Check the TOKEN_MINT value in .env or pass --mint with a valid address.",
    );
  }

  const mintInfo = await fetchMint(connection, mint);

  if (!quiet && !jsonOutput) {
    log.success("Mint exists on-chain");
    log.kv("Decimals", mintInfo.decimals);
    log.kv(
      "Total Supply",
      fromRawAmount(mintInfo.supply, mintInfo.decimals).toString(),
    );
    log.blank();
  }

  // ── Run verification ─────────────────────────────────────────────────

  if (!quiet && !jsonOutput) {
    log.step(
      2,
      3,
      `Verifying ownership for ${walletPubkeys.length} wallet(s)…`,
    );
    log.blank();
  }

  const entries: VerificationEntry[] = [];

  if (walletPubkeys.length === 1) {
    // Single wallet – use direct verification for better error messages.
    const wallet = walletPubkeys[0]!;
    try {
      const result = await verifyOwnership(
        connection,
        wallet,
        mint,
        minBalance,
      );
      entries.push({
        wallet: wallet.toBase58(),
        isOwner: result.isOwner,
        rawBalance: result.rawBalance.toString(),
        uiBalance: result.uiBalance,
        requiredBalance: minBalance,
        ata: result.ata.toBase58(),
        decimals: result.decimals,
      });
    } catch (err) {
      entries.push({
        wallet: wallet.toBase58(),
        isOwner: false,
        rawBalance: "0",
        uiBalance: 0,
        requiredBalance: minBalance,
        ata: "",
        decimals: mintInfo.decimals,
        error: err instanceof Error ? err.message : String(err),
      });
    }
  } else {
    // Batch verification.
    const resultsMap = await verifyOwnershipBatch(
      connection,
      walletPubkeys,
      mint,
      minBalance,
    );

    for (const wallet of walletPubkeys) {
      const key = wallet.toBase58();
      const result = resultsMap.get(key);
      if (result) {
        entries.push({
          wallet: key,
          isOwner: result.isOwner,
          rawBalance: result.rawBalance.toString(),
          uiBalance: result.uiBalance,
          requiredBalance: minBalance,
          ata: result.ata.toBase58(),
          decimals: result.decimals,
        });
      } else {
        entries.push({
          wallet: key,
          isOwner: false,
          rawBalance: "0",
          uiBalance: 0,
          requiredBalance: minBalance,
          ata: "",
          decimals: mintInfo.decimals,
          error: "Verification did not return a result (RPC issue or timeout).",
        });
      }
    }
  }

  // ── Build report ─────────────────────────────────────────────────────

  const totalPassed = entries.filter((e) => e.isOwner).length;
  const totalFailed = entries.length - totalPassed;

  const report: VerificationReport = {
    mint: mint.toBase58(),
    minBalance,
    cluster: cfg.cluster,
    timestamp: new Date().toISOString(),
    totalChecked: entries.length,
    totalPassed,
    totalFailed,
    results: entries,
  };

  // ── Output ───────────────────────────────────────────────────────────

  if (!quiet && !jsonOutput) {
    log.step(3, 3, "Results");
    log.blank();
  }

  if (jsonOutput) {
    printReportJSON(report);
  } else if (quiet) {
    // In quiet mode, only print a single summary line.
    if (totalFailed === 0) {
      console.log("PASS");
    } else {
      console.log("FAIL");
    }
  } else {
    printReport(report, verbose);

    // Helpful next steps.
    if (totalFailed > 0) {
      log.blank();
      log.info("To fix failing wallets, mint tokens to them:");
      for (const entry of entries) {
        if (!entry.isOwner && !entry.error) {
          log.kv(
            "Mint",
            `npx tsx src/mint-tokens.ts --to ${entry.wallet} --amount ${minBalance}`,
          );
        }
      }
      log.blank();
    }

    log.info("Related commands:");
    log.kv("Token info", "npx tsx src/token-info.ts");
    log.kv(
      "Mint tokens",
      "npx tsx src/mint-tokens.ts --to <wallet> --amount <n>",
    );
    log.kv(
      "Transfer",
      "npx tsx src/transfer-tokens.ts --to <wallet> --amount <n>",
    );
    log.kv("Holders", "npx tsx src/token-info.ts --holders");
    log.kv("Snapshot all", "npx tsx src/snapshot-holders.ts --min-balance 1");
    log.blank();
  }

  // ── Exit code ────────────────────────────────────────────────────────

  // Exit with code 1 if any wallet failed verification.  This allows
  // the script to be used in CI/CD pipelines and health-check scripts.
  if (totalFailed > 0) {
    process.exit(1);
  }
}

main().catch((err) => {
  log.error("Ownership verification failed:", err);
  process.exit(1);
});
