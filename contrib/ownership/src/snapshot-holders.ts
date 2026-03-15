/**
 * OpenTela Ownership Tools – Snapshot Holders
 *
 * Takes a complete snapshot of all wallets that hold the OpenTela SPL
 * ownership token.  This is useful for auditing, governance snapshots,
 * airdrop eligibility lists, and verifying the distribution of ownership
 * tokens across the network.
 *
 * The snapshot is produced by querying `getProgramAccounts` on the SPL
 * Token Program with a filter for the configured mint address.  This
 * returns every token account for the mint, from which we extract the
 * owner wallet and balance.
 *
 * Usage:
 *   npx tsx src/snapshot-holders.ts
 *   npx tsx src/snapshot-holders.ts --mint <pubkey>
 *   npx tsx src/snapshot-holders.ts --min-balance 1
 *   npx tsx src/snapshot-holders.ts --out snapshot.json
 *   npx tsx src/snapshot-holders.ts --out snapshot.csv --format csv
 *   npx tsx src/snapshot-holders.ts --top 50
 *
 * Options:
 *   --mint <pubkey>       Token mint address (overrides TOKEN_MINT from .env).
 *   --min-balance <n>     Only include holders with at least this many
 *                          whole tokens (default: 0, meaning all non-zero
 *                          holders are included).
 *   --top <n>             Only show the top N holders by balance.  When
 *                          omitted, all holders are included.
 *   --out <path>          Write the snapshot to a file instead of stdout.
 *   --format <fmt>        Output format: "json" (default), "csv", or "table".
 *   --include-zero        Include token accounts with zero balance (these
 *                          are normally filtered out).
 *   --json                Alias for --format json.
 *   --csv                 Alias for --format csv.
 *   --quiet               Suppress informational output; only emit the
 *                          snapshot data (useful for piping).
 *
 * Environment / .env:
 *   TOKEN_MINT, SOLANA_CLUSTER, KEYPAIR_PATH, etc.
 */

import { Command } from "commander";
import { PublicKey } from "@solana/web3.js";
import { writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { loadConfig, getConnection, printConfig } from "./config.js";
import {
  fetchMint,
  mintExists,
  fromRawAmount,
  getTokenSupply,
  snapshotAllHolders,
} from "./spl-helpers.js";
import * as log from "./log.js";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface SnapshotEntry {
  rank: number;
  owner: string;
  tokenAccount: string;
  rawBalance: string;
  uiBalance: number;
  percentageOfSupply: number;
}

interface SnapshotReport {
  mint: string;
  cluster: string;
  timestamp: string;
  decimals: number;
  totalSupply: {
    raw: string;
    ui: number;
  };
  minBalance: number;
  totalHolders: number;
  includedHolders: number;
  entries: SnapshotEntry[];
}

type OutputFormat = "json" | "csv" | "table";

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

function resolveFormat(opts: Record<string, unknown>): OutputFormat {
  if (opts.csv === true) return "csv";
  if (opts.json === true) return "json";

  const fmt = opts.format as string | undefined;
  if (fmt) {
    const normalised = fmt.trim().toLowerCase();
    if (
      normalised === "json" ||
      normalised === "csv" ||
      normalised === "table"
    ) {
      return normalised;
    }
    log.fatal(
      `Invalid output format: "${fmt}". ` +
        `Accepted values: json, csv, table.`,
    );
  }

  return "json";
}

// ---------------------------------------------------------------------------
// Formatters
// ---------------------------------------------------------------------------

function formatJSON(report: SnapshotReport): string {
  return JSON.stringify(report, null, 2);
}

function formatCSV(report: SnapshotReport): string {
  const header =
    "rank,owner,token_account,raw_balance,ui_balance,pct_of_supply";
  const rows = report.entries.map(
    (e) =>
      `${e.rank},${e.owner},${e.tokenAccount},${e.rawBalance},${e.uiBalance},${e.percentageOfSupply.toFixed(6)}`,
  );
  return [header, ...rows].join("\n") + "\n";
}

function formatTable(report: SnapshotReport, _cluster: string): string {
  const lines: string[] = [];

  lines.push("");
  lines.push("  Snapshot – Token Holders");
  lines.push(`  Mint: ${report.mint}`);
  lines.push(`  Cluster: ${report.cluster}`);
  lines.push(`  Timestamp: ${report.timestamp}`);
  lines.push(
    `  Total Supply: ${report.totalSupply.ui} (raw: ${report.totalSupply.raw})`,
  );
  lines.push(`  Min Balance Filter: ${report.minBalance}`);
  lines.push(`  Total Holders (non-zero): ${report.totalHolders}`);
  lines.push(`  Included in snapshot: ${report.includedHolders}`);
  lines.push("");

  if (report.entries.length === 0) {
    lines.push("  (no holders found)");
    lines.push("");
    return lines.join("\n");
  }

  // Column widths
  const rankW = 6;
  const ownerW = 46;
  const balanceW = 20;
  const pctW = 10;

  const hdr =
    "Rank".padEnd(rankW) +
    "Owner".padEnd(ownerW) +
    "Balance".padStart(balanceW) +
    "% Supply".padStart(pctW);

  lines.push(`  ${hdr}`);
  lines.push(`  ${"─".repeat(hdr.length)}`);

  for (const entry of report.entries) {
    const rank = `#${entry.rank}`.padEnd(rankW);
    const owner = entry.owner.padEnd(ownerW);
    const balance = entry.uiBalance.toString().padStart(balanceW);
    const pct = `${entry.percentageOfSupply.toFixed(4)}%`.padStart(pctW);
    lines.push(`  ${rank}${owner}${balance}${pct}`);
  }

  lines.push("");
  lines.push(
    `  Showing ${report.entries.length} of ${report.totalHolders} holder(s).`,
  );
  lines.push("");

  return lines.join("\n");
}

// ---------------------------------------------------------------------------
// Display helpers (for non-quiet mode)
// ---------------------------------------------------------------------------

function printSummary(report: SnapshotReport): void {
  log.header("Snapshot Summary");

  log.kv("Mint", report.mint);
  log.kv("Cluster", report.cluster);
  log.kv("Timestamp", report.timestamp);
  log.kv("Decimals", report.decimals);
  log.blank();

  log.kv("Total Supply (raw)", report.totalSupply.raw);
  log.kv("Total Supply (UI)", report.totalSupply.ui.toString());
  log.blank();

  log.kv("Total Non-Zero Holders", report.totalHolders);
  log.kv("Included (after filters)", report.includedHolders);
  log.kv("Min Balance Filter", `${report.minBalance} token(s)`);
  log.blank();

  // Top 5 summary (if we have entries)
  if (report.entries.length > 0) {
    log.info("Top holders:");
    const top = report.entries.slice(0, 5);
    for (const entry of top) {
      log.kv(
        `  #${entry.rank}`,
        `${entry.owner}  ${entry.uiBalance} (${entry.percentageOfSupply.toFixed(2)}%)`,
      );
    }
    if (report.entries.length > 5) {
      log.info(`  … and ${report.entries.length - 5} more.`);
    }
    log.blank();
  }
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function main(): Promise<void> {
  const program = new Command()
    .name("snapshot-holders")
    .description(
      "Take a snapshot of all wallets holding the OpenTela ownership token",
    )
    .option("--mint <pubkey>", "Token mint address (overrides TOKEN_MINT)")
    .option(
      "--min-balance <n>",
      "Only include holders with at least this many whole tokens",
      parseFloat,
    )
    .option("--top <n>", "Only include the top N holders by balance", parseInt)
    .option("--out <path>", "Write the snapshot to a file instead of stdout")
    .option("--format <fmt>", "Output format: json, csv, or table")
    .option("--json", "Alias for --format json")
    .option("--csv", "Alias for --format csv")
    .option("--include-zero", "Include token accounts with zero balance")
    .option("--quiet", "Suppress informational output; only emit snapshot data")
    .parse(process.argv);

  const opts = program.opts();

  // ── Configuration ────────────────────────────────────────────────────

  const cfg = loadConfig();
  if (cfg.debug) log.setDebug(true);

  const connection = getConnection(cfg);
  const quiet = opts.quiet === true;
  const includeZero = opts.includeZero === true;
  const format = resolveFormat(opts as Record<string, unknown>);
  const minBalance = (opts.minBalance as number | undefined) ?? 0;
  const topN = opts.top as number | undefined;
  const outPath = opts.out as string | undefined;

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

  // ── Print header ─────────────────────────────────────────────────────

  if (!quiet) {
    log.header("OpenTela Token Holder Snapshot");
    printConfig(cfg);
    log.blank();
    log.kv("Mint", mint.toBase58());
    log.kv("Output Format", format);
    log.kv("Min Balance", `${minBalance} token(s)`);
    log.kv("Top N", topN !== undefined ? topN.toString() : "(all)");
    log.kv("Include Zero", includeZero ? "yes" : "no");
    log.kv("Output File", outPath ?? "(stdout)");
    log.blank();
  }

  // ── Validate mint exists ─────────────────────────────────────────────

  const totalSteps = 4;

  if (!quiet) {
    log.step(1, totalSteps, "Validating mint account…");
  }

  const exists = await mintExists(connection, mint);
  if (!exists) {
    log.fatal(
      `Mint ${mint.toBase58()} does not exist on-chain.\n` +
        "  Check the TOKEN_MINT value in .env or pass --mint with a valid address.",
    );
  }

  const mintInfo = await fetchMint(connection, mint);
  const supply = await getTokenSupply(connection, mint);

  if (!quiet) {
    log.success("Mint exists on-chain");
    log.kv("Decimals", mintInfo.decimals);
    log.kv("Total Supply (raw)", supply.raw.toString());
    log.kv("Total Supply (UI)", supply.ui.toString());
    log.blank();
  }

  // ── Fetch all holders ────────────────────────────────────────────────

  if (!quiet) {
    log.step(
      2,
      totalSteps,
      "Querying all token accounts (this may take a moment for widely-held tokens)…",
    );
  }

  const rawHolders = await snapshotAllHolders(connection, mint);

  if (!quiet) {
    log.success(
      `Found ${rawHolders.length} token account(s) with non-zero balance.`,
    );
    log.blank();
  }

  // ── Filter and sort ──────────────────────────────────────────────────

  if (!quiet) {
    log.step(3, totalSteps, "Filtering and sorting…");
  }

  // Aggregate by owner (a single owner might have multiple token accounts,
  // though typically they'll only have one ATA).
  const ownerMap = new Map<
    string,
    { owner: PublicKey; tokenAccount: PublicKey; rawBalance: bigint }
  >();

  for (const holder of rawHolders) {
    const ownerKey = holder.owner.toBase58();
    const existing = ownerMap.get(ownerKey);

    if (existing) {
      // Aggregate balances and keep the token account with the highest
      // balance as the "representative" account.
      const newTotal = existing.rawBalance + holder.rawBalance;
      ownerMap.set(ownerKey, {
        owner: holder.owner,
        tokenAccount:
          holder.rawBalance > existing.rawBalance
            ? holder.tokenAccount
            : existing.tokenAccount,
        rawBalance: newTotal,
      });
    } else {
      ownerMap.set(ownerKey, {
        owner: holder.owner,
        tokenAccount: holder.tokenAccount,
        rawBalance: holder.rawBalance,
      });
    }
  }

  // Convert to sorted array.
  let holders = Array.from(ownerMap.values());

  // Sort descending by balance.
  holders.sort((a, b) => {
    if (a.rawBalance > b.rawBalance) return -1;
    if (a.rawBalance < b.rawBalance) return 1;
    return 0;
  });

  // Total non-zero holders before filtering.
  const totalNonZeroHolders = includeZero ? rawHolders.length : holders.length;

  // Apply min-balance filter.
  if (minBalance > 0) {
    const minRaw = BigInt(Math.round(minBalance * 10 ** mintInfo.decimals));
    holders = holders.filter((h) => h.rawBalance >= minRaw);
  }

  // Filter out zero-balance accounts unless --include-zero is set.
  if (!includeZero) {
    holders = holders.filter((h) => h.rawBalance > BigInt(0));
  }

  // Apply --top limit.
  if (topN !== undefined && topN > 0) {
    holders = holders.slice(0, topN);
  }

  if (!quiet) {
    log.success(
      `${holders.length} holder(s) after filtering ` +
        `(from ${totalNonZeroHolders} total).`,
    );
    log.blank();
  }

  // ── Build report ─────────────────────────────────────────────────────

  if (!quiet) {
    log.step(4, totalSteps, "Building snapshot…");
  }

  const supplyRaw = supply.raw > BigInt(0) ? supply.raw : BigInt(1);

  const entries: SnapshotEntry[] = holders.map((h, idx) => {
    const uiBalance = fromRawAmount(h.rawBalance, mintInfo.decimals);
    const percentage =
      supply.raw > BigInt(0)
        ? Number((h.rawBalance * BigInt(10000)) / supplyRaw) / 100
        : 0;

    return {
      rank: idx + 1,
      owner: h.owner.toBase58(),
      tokenAccount: h.tokenAccount.toBase58(),
      rawBalance: h.rawBalance.toString(),
      uiBalance,
      percentageOfSupply: percentage,
    };
  });

  const report: SnapshotReport = {
    mint: mint.toBase58(),
    cluster: cfg.cluster,
    timestamp: new Date().toISOString(),
    decimals: mintInfo.decimals,
    totalSupply: {
      raw: supply.raw.toString(),
      ui: supply.ui,
    },
    minBalance,
    totalHolders: totalNonZeroHolders,
    includedHolders: entries.length,
    entries,
  };

  // ── Output ───────────────────────────────────────────────────────────

  let outputContent: string;

  switch (format) {
    case "csv":
      outputContent = formatCSV(report);
      break;
    case "table":
      outputContent = formatTable(report, cfg.cluster);
      break;
    case "json":
    default:
      outputContent = formatJSON(report);
      break;
  }

  if (outPath) {
    // Write to file.
    const absPath = resolve(outPath);
    try {
      writeFileSync(absPath, outputContent, "utf-8");
      if (!quiet) {
        log.blank();
        log.success(`Snapshot written to ${absPath}`);
        log.kv("Format", format);
        log.kv("Holders", entries.length);
        log.kv(
          "File Size",
          `${Buffer.byteLength(outputContent, "utf-8")} bytes`,
        );
        log.blank();
      }
    } catch (err) {
      log.fatal(
        `Failed to write snapshot to ${absPath}: ` +
          `${err instanceof Error ? err.message : err}`,
      );
    }
  } else {
    // Print to stdout.
    if (!quiet && format !== "json") {
      // For non-JSON, non-quiet mode: print the formatted output directly.
      console.log(outputContent);
    } else {
      // For JSON or quiet mode: print raw output to stdout.
      process.stdout.write(outputContent);
      if (!outputContent.endsWith("\n")) {
        process.stdout.write("\n");
      }
    }
  }

  // ── Summary (non-quiet mode) ─────────────────────────────────────────

  if (!quiet && !outPath) {
    // Only print summary when outputting to file or in non-quiet mode
    // with table/csv output (JSON already contains all info).
    if (format !== "json") {
      printSummary(report);
    }
  } else if (!quiet && outPath) {
    printSummary(report);
  }

  // ── Done ─────────────────────────────────────────────────────────────

  if (!quiet) {
    log.divider();
    log.success("Snapshot complete!");
    log.blank();

    // Distribution statistics.
    if (entries.length > 0) {
      const topHolder = entries[0]!;
      const medianIdx = Math.floor(entries.length / 2);
      const medianHolder = entries[medianIdx]!;
      const bottomHolder = entries[entries.length - 1]!;

      log.info("Distribution statistics:");
      log.kv(
        "Largest Holder",
        `${topHolder.owner} (${topHolder.uiBalance} – ${topHolder.percentageOfSupply.toFixed(2)}%)`,
      );
      log.kv(
        "Median Holder",
        `${medianHolder.owner} (${medianHolder.uiBalance} – ${medianHolder.percentageOfSupply.toFixed(2)}%)`,
      );
      log.kv(
        "Smallest Holder",
        `${bottomHolder.owner} (${bottomHolder.uiBalance} – ${bottomHolder.percentageOfSupply.toFixed(2)}%)`,
      );

      // Concentration: top 10% holders' share of supply.
      const top10Pct = Math.max(1, Math.ceil(entries.length * 0.1));
      const top10PctEntries = entries.slice(0, top10Pct);
      const top10PctShare = top10PctEntries.reduce(
        (acc, e) => acc + e.percentageOfSupply,
        0,
      );
      log.kv(
        `Top ${top10Pct} (${Math.round((top10Pct / entries.length) * 100)}%)`,
        `hold ${top10PctShare.toFixed(2)}% of supply`,
      );
      log.blank();
    }

    log.info("Related commands:");
    log.kv("Token info", "npx tsx src/token-info.ts");
    log.kv(
      "Verify ownership",
      "npx tsx src/verify-ownership.ts --wallet <pubkey>",
    );
    log.kv("Top holders (quick)", "npx tsx src/token-info.ts --holders");
    log.kv(
      "Mint tokens",
      "npx tsx src/mint-tokens.ts --to <wallet> --amount <n>",
    );
    log.kv(
      "Transfer tokens",
      "npx tsx src/transfer-tokens.ts --to <wallet> --amount <n>",
    );
    log.blank();
  }
}

main().catch((err) => {
  log.error("Snapshot failed:", err);
  process.exit(1);
});
