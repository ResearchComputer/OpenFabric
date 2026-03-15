/**
 * OpenTela Ownership Tools – Create ATA
 *
 * Creates Associated Token Accounts (ATAs) for one or more wallets.
 * An ATA is a deterministic token account derived from a wallet address
 * and a token mint — it must exist before a wallet can receive SPL tokens.
 *
 * Most OpenTela ownership scripts (mint-tokens, transfer-tokens, etc.)
 * create ATAs automatically when needed.  This standalone script is
 * useful when you want to pre-provision ATAs for a list of wallets
 * (e.g. before a batch airdrop) or simply inspect whether they exist.
 *
 * Usage:
 *   npx tsx src/create-ata.ts --wallet <pubkey>
 *   npx tsx src/create-ata.ts --wallet <pubkey1> --wallet <pubkey2>
 *   npx tsx src/create-ata.ts --file wallets.txt
 *   npx tsx src/create-ata.ts --wallet <pubkey> --check-only
 *
 * Options:
 *   --wallet <pubkey>   Wallet public key(s) to create ATAs for.  Can be
 *                        specified multiple times.
 *   --file <path>       Path to a newline-delimited file of wallet public
 *                        keys.
 *   --stdin             Read wallet public keys from stdin (one per line).
 *   --mint <pubkey>     Token mint address (overrides TOKEN_MINT from .env).
 *   --check-only        Only check whether the ATAs exist — do not create
 *                        them.  Exit code 0 = all exist, 1 = some missing.
 *   --json              Output results as JSON instead of human-readable text.
 *   --dry-run           Print what would happen without sending transactions.
 *
 * Environment / .env:
 *   TOKEN_MINT, SOLANA_CLUSTER, KEYPAIR_PATH, etc.
 */

import { Command } from "commander";
import { PublicKey } from "@solana/web3.js";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { loadConfig, getConnection, printConfig } from "./config.js";
import {
  deriveATA,
  ataExists,
  getOrCreateATA,
  mintExists,
  fetchMint,
  fromRawAmount,
  getSolBalance,
} from "./spl-helpers.js";
import * as log from "./log.js";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface ATAEntry {
  wallet: string;
  ata: string;
  existed: boolean;
  created: boolean;
  balance: string | null;
  error?: string;
}

interface ATAReport {
  mint: string;
  cluster: string;
  timestamp: string;
  totalWallets: number;
  alreadyExisted: number;
  created: number;
  failed: number;
  results: ATAEntry[];
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

function printReport(report: ATAReport, cluster: string): void {
  log.header("ATA Creation Report");

  log.kv("Mint", report.mint);
  log.kv("Cluster", report.cluster);
  log.kv("Timestamp", report.timestamp);
  log.blank();

  log.kv("Total Wallets", report.totalWallets);
  log.kv("Already Existed", report.alreadyExisted);
  log.kv("Newly Created", report.created);
  log.kv("Failed", report.failed);
  log.blank();

  if (report.results.length > 0) {
    log.divider();
    log.blank();

    for (const entry of report.results) {
      if (entry.error) {
        log.error(`${entry.wallet}`);
        log.kv("Error", entry.error, 6);
        log.blank();
        continue;
      }

      const statusIcon = entry.created ? "+" : entry.existed ? "=" : "?";
      const statusLabel = entry.created
        ? "CREATED"
        : entry.existed
          ? "EXISTS"
          : "UNKNOWN";

      const balanceStr =
        entry.balance !== null ? `  balance: ${entry.balance}` : "";

      console.log(
        `  [${statusIcon}] ${statusLabel.padEnd(8)} ${entry.wallet}${balanceStr}`,
      );
      log.kv("ATA", entry.ata, 6);
      log.kv(
        "Explorer",
        log.explorerAddressUrl(entry.ata, cluster),
        6,
      );
      log.blank();
    }
  }

  log.divider();

  if (report.failed === 0) {
    log.success(
      `All ${report.totalWallets} ATA(s) are ready ` +
        `(${report.alreadyExisted} existed, ${report.created} created).`,
    );
  } else {
    log.warn(
      `${report.failed} of ${report.totalWallets} ATA(s) could not be created.`,
    );
  }

  log.blank();
}

function printReportJSON(report: ATAReport): void {
  console.log(JSON.stringify(report, null, 2));
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function main(): Promise<void> {
  const program = new Command()
    .name("create-ata")
    .description(
      "Create Associated Token Accounts (ATAs) for wallet(s) and the OpenTela ownership token",
    )
    .option(
      "--wallet <pubkey...>",
      "Wallet public key(s) to create ATAs for (repeatable)",
    )
    .option("--file <path>", "Newline-delimited file of wallet public keys")
    .option("--stdin", "Read wallet public keys from stdin")
    .option("--mint <pubkey>", "Token mint address (overrides TOKEN_MINT)")
    .option(
      "--check-only",
      "Only check whether ATAs exist — do not create them",
    )
    .option("--json", "Output results as JSON")
    .option("--dry-run", "Print plan without sending transactions")
    .parse(process.argv);

  const opts = program.opts();

  // ── Configuration ────────────────────────────────────────────────────

  const cfg = loadConfig();
  if (cfg.debug) log.setDebug(true);

  const connection = getConnection(cfg);
  const payer = cfg.keypair;
  const jsonOutput = opts.json === true;
  const checkOnly = opts.checkOnly === true;
  const dryRun = opts.dryRun === true;

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

  // ── Collect wallets ──────────────────────────────────────────────────

  const walletStrings = collectWallets(opts as Record<string, unknown>);

  if (walletStrings.length === 0) {
    // Default to the payer wallet when no wallets are specified.
    const defaultWallet = payer.publicKey.toBase58();
    if (!jsonOutput) {
      log.info(
        `No wallets specified – using the authority wallet: ${defaultWallet}`,
      );
      log.blank();
    }
    walletStrings.push(defaultWallet);
  }

  // Validate all wallet addresses up front.
  const validWallets: { address: string; pubkey: PublicKey }[] = [];
  for (const ws of walletStrings) {
    try {
      const pk = new PublicKey(ws);
      validWallets.push({ address: ws, pubkey: pk });
    } catch {
      if (!jsonOutput) {
        log.warn(`Skipping invalid public key: "${ws}"`);
      }
    }
  }

  if (validWallets.length === 0) {
    log.fatal("No valid wallet addresses to process.");
  }

  // ── Print header ─────────────────────────────────────────────────────

  const mode = checkOnly ? "Check" : dryRun ? "Dry Run" : "Create";

  if (!jsonOutput) {
    log.header(`${mode} Associated Token Accounts`);
    printConfig(cfg);
    log.blank();
    log.kv("Mint", mint.toBase58());
    log.kv("Mode", checkOnly ? "check-only" : dryRun ? "dry-run" : "create");
    log.kv("Wallets", validWallets.length);
    log.blank();
  }

  // ── Validate mint exists ─────────────────────────────────────────────

  const totalSteps = checkOnly || dryRun ? 3 : 4;

  if (!jsonOutput) {
    log.step(1, totalSteps, "Validating mint account…");
  }

  const exists = await mintExists(connection, mint);
  if (!exists) {
    log.fatal(
      `Mint ${mint.toBase58()} does not exist on-chain.\n` +
        "  Check the TOKEN_MINT value in .env or pass --mint with a valid address.\n" +
        "  Run `npx tsx src/create-token.ts` to create one first.",
    );
  }

  const mintInfo = await fetchMint(connection, mint);

  if (!jsonOutput) {
    log.success("Mint exists on-chain");
    log.kv("Decimals", mintInfo.decimals);
    log.kv(
      "Total Supply",
      fromRawAmount(mintInfo.supply, mintInfo.decimals).toString(),
    );
    log.blank();
  }

  // ── Check payer SOL balance (only when creating) ─────────────────────

  if (!checkOnly && !dryRun && !jsonOutput) {
    log.step(2, totalSteps, "Checking payer SOL balance…");
    const solBalance = await getSolBalance(connection, payer.publicKey);
    log.kv("Payer", payer.publicKey.toBase58());
    log.kv("SOL Balance", `${solBalance.toFixed(9)} SOL`);

    // Rough estimate: each ATA creation costs ~0.002 SOL for rent.
    const estimatedCost = validWallets.length * 0.00204;
    log.kv("Estimated Cost", `~${estimatedCost.toFixed(4)} SOL (worst case)`);

    if (solBalance < estimatedCost) {
      log.warn(
        `Payer SOL balance may be insufficient to create ${validWallets.length} ATA(s). ` +
          `Each ATA costs ~0.002 SOL in rent-exempt reserve.`,
      );
    }
    log.blank();
  }

  // ── Process each wallet ──────────────────────────────────────────────

  const stepNum = checkOnly || dryRun ? 2 : 3;
  if (!jsonOutput) {
    log.step(
      stepNum,
      totalSteps,
      checkOnly
        ? "Checking ATA existence…"
        : dryRun
          ? "Computing ATA addresses (dry run)…"
          : "Creating ATAs…",
    );
    log.blank();
  }

  const entries: ATAEntry[] = [];
  let alreadyExisted = 0;
  let created = 0;
  let failed = 0;

  for (let i = 0; i < validWallets.length; i++) {
    const { address, pubkey } = validWallets[i]!;
    const ata = deriveATA(pubkey, mint);
    const ataStr = ata.toBase58();

    log.debug(`[${i + 1}/${validWallets.length}] Processing ${address} → ATA ${ataStr}`);

    try {
      const exists = await ataExists(connection, pubkey, mint);

      if (exists) {
        // ATA already exists — fetch balance for informational purposes.
        alreadyExisted++;
        let balance: string | null = null;
        try {
          const { getAccount } = await import("@solana/spl-token");
          const account = await getAccount(connection, ata, "confirmed");
          balance = fromRawAmount(account.amount, mintInfo.decimals).toString();
        } catch {
          // Couldn't fetch balance — not critical.
        }

        entries.push({
          wallet: address,
          ata: ataStr,
          existed: true,
          created: false,
          balance,
        });

        if (!jsonOutput && !checkOnly) {
          log.debug(`  ATA ${ataStr} already exists (balance: ${balance ?? "unknown"})`);
        }
        continue;
      }

      // ATA does not exist.
      if (checkOnly) {
        // In check-only mode, record as missing but don't create.
        failed++;
        entries.push({
          wallet: address,
          ata: ataStr,
          existed: false,
          created: false,
          balance: null,
        });
        continue;
      }

      if (dryRun) {
        // In dry-run mode, record what would happen.
        entries.push({
          wallet: address,
          ata: ataStr,
          existed: false,
          created: false, // would be created
          balance: null,
        });
        if (!jsonOutput) {
          log.info(`  Would create ATA ${ataStr} for ${address}`);
        }
        continue;
      }

      // Actually create the ATA.
      if (!jsonOutput) {
        log.info(`  Creating ATA for ${address}…`);
      }

      const account = await getOrCreateATA(connection, payer, mint, pubkey);
      created++;

      entries.push({
        wallet: address,
        ata: account.address.toBase58(),
        existed: false,
        created: true,
        balance: "0",
      });

      if (!jsonOutput) {
        log.success(`  ATA created: ${account.address.toBase58()}`);
      }
    } catch (err) {
      failed++;
      const errMsg = err instanceof Error ? err.message : String(err);
      entries.push({
        wallet: address,
        ata: ataStr,
        existed: false,
        created: false,
        balance: null,
        error: errMsg,
      });

      if (!jsonOutput) {
        log.error(`  Failed to process ${address}: ${errMsg}`);
      }
    }
  }

  // ── Build report ─────────────────────────────────────────────────────

  const report: ATAReport = {
    mint: mint.toBase58(),
    cluster: cfg.cluster,
    timestamp: new Date().toISOString(),
    totalWallets: validWallets.length,
    alreadyExisted,
    created,
    failed,
    results: entries,
  };

  // ── Output ───────────────────────────────────────────────────────────

  if (!jsonOutput) {
    log.blank();
    const finalStep = checkOnly || dryRun ? 3 : 4;
    log.step(finalStep, totalSteps, "Summary");
    log.blank();
  }

  if (jsonOutput) {
    printReportJSON(report);
  } else {
    printReport(report, cfg.cluster);

    // Next steps.
    if (!checkOnly && !dryRun) {
      log.info("Next steps:");
      log.kv("Mint tokens", "npx tsx src/mint-tokens.ts --to <wallet> --amount <n>");
      log.kv("Transfer", "npx tsx src/transfer-tokens.ts --to <wallet> --amount <n>");
      log.kv(
        "Verify ownership",
        "npx tsx src/verify-ownership.ts --wallet <pubkey>",
      );
      log.blank();
    } else if (checkOnly && failed > 0) {
      log.blank();
      log.info("To create the missing ATAs, run without --check-only:");
      const missingWallets = entries
        .filter((e) => !e.existed)
        .map((e) => `--wallet ${e.wallet}`)
        .join(" ");
      console.log(`  npx tsx src/create-ata.ts ${missingWallets}`);
      log.blank();
    } else if (dryRun) {
      const toCreate = entries.filter((e) => !e.existed).length;
      if (toCreate > 0) {
        log.blank();
        log.info(
          `Dry run complete. ${toCreate} ATA(s) would be created. ` +
            "Remove --dry-run to execute.",
        );
        log.blank();
      }
    }
  }

  // ── Exit code ────────────────────────────────────────────────────────

  // In check-only mode, exit 1 if any ATA is missing.
  // In create mode, exit 1 if any creation failed.
  if (checkOnly && failed > 0) {
    process.exit(1);
  }
  if (!checkOnly && !dryRun && failed > 0) {
    process.exit(1);
  }
}

main().catch((err) => {
  log.error("ATA operation failed:", err);
  process.exit(1);
});
