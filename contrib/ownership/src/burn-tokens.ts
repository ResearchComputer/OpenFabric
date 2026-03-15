/**
 * OpenTela Ownership Tools – Burn Tokens
 *
 * Burns SPL tokens from the authority wallet, permanently removing them
 * from circulation and reducing the effective supply.  Burned tokens are
 * destroyed at the token-account level — the mint's total supply counter
 * is decremented accordingly.
 *
 * Usage:
 *   npx tsx src/burn-tokens.ts --amount <n>
 *   npx tsx src/burn-tokens.ts --amount <n> --mint <pubkey>
 *   npx tsx src/burn-tokens.ts --amount 500000000 --raw
 *   npx tsx src/burn-tokens.ts --amount <n> --dry-run
 *
 * Options:
 *   --amount <n>        Amount to burn in whole tokens (e.g. 100).
 *                        With --raw this is interpreted as a raw integer
 *                        (i.e. value already multiplied by 10^decimals).
 *   --mint <pubkey>     Token mint address (overrides TOKEN_MINT from .env).
 *   --raw               Treat --amount as a raw integer (skip decimal
 *                        conversion).
 *   --yes               Skip the interactive confirmation prompt.
 *   --dry-run           Print what would happen without sending transactions.
 *
 * Environment / .env:
 *   TOKEN_MINT, TOKEN_DECIMALS, SOLANA_CLUSTER, KEYPAIR_PATH, etc.
 *
 * ⚠  WARNING: Burning tokens is PERMANENT.  The burned tokens cannot be
 *    recovered.  If the mint authority has been revoked, those tokens are
 *    gone forever.
 */

import { Command } from "commander";
import { PublicKey } from "@solana/web3.js";
import { createInterface } from "node:readline";
import { loadConfig, getConnection, printConfig } from "./config.js";
import {
  burnTokens,
  toRawAmount,
  fromRawAmount,
  fetchMint,
  mintExists,
  deriveATA,
  ataExists,
  getTokenBalance,
  getTokenSupply,
  getSolBalance,
} from "./spl-helpers.js";
import * as log from "./log.js";

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
 * Prompt the user for interactive confirmation via stdin.
 *
 * Returns `true` if the user types "yes" or "y" (case-insensitive),
 * `false` otherwise.
 */
async function confirmPrompt(message: string): Promise<boolean> {
  const rl = createInterface({
    input: process.stdin,
    output: process.stdout,
  });

  return new Promise<boolean>((resolve) => {
    rl.question(message, (answer) => {
      rl.close();
      const normalised = (answer ?? "").trim().toLowerCase();
      resolve(normalised === "yes" || normalised === "y");
    });
  });
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function main(): Promise<void> {
  const program = new Command()
    .name("burn-tokens")
    .description(
      "Burn OpenTela SPL tokens from your wallet (⚠ permanent, irreversible)",
    )
    .requiredOption(
      "--amount <n>",
      "Amount to burn (whole tokens unless --raw)",
    )
    .option("--mint <pubkey>", "Token mint address (overrides TOKEN_MINT)")
    .option("--raw", "Treat amount as raw integer (no decimal conversion)")
    .option("--yes", "Skip interactive confirmation prompt")
    .option("--dry-run", "Print plan without sending transactions")
    .parse(process.argv);

  const opts = program.opts();

  // ── Configuration ────────────────────────────────────────────────────

  const cfg = loadConfig();
  if (cfg.debug) log.setDebug(true);

  const connection = getConnection(cfg);
  const owner = cfg.keypair;
  const isRaw = opts.raw === true;
  const skipConfirmation = opts.yes === true;
  const dryRun = opts.dryRun === true;
  const amountStr = opts.amount as string;

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

  log.header("⚠  Burn OpenTela SPL Tokens (Irreversible)");
  printConfig(cfg);
  log.blank();

  // ── Step 1: Validate mint exists ─────────────────────────────────────

  const totalSteps = 5;

  log.step(1, totalSteps, "Validating mint account…");

  const exists = await mintExists(connection, mint);
  if (!exists) {
    log.fatal(
      `Mint ${mint.toBase58()} does not exist on-chain.\n` +
        "  Did you deploy it?  Run `npx tsx src/create-token.ts` first.",
    );
  }

  const mintInfo = await fetchMint(connection, mint);
  const decimals = mintInfo.decimals;
  const supplyBefore = await getTokenSupply(connection, mint);

  log.success("Mint exists on-chain");
  log.kv("Mint", mint.toBase58());
  log.kv("Explorer", log.explorerAddressUrl(mint.toBase58(), cfg.cluster));
  log.kv("Decimals", decimals);
  log.kv("Current Supply (raw)", supplyBefore.raw.toString());
  log.kv("Current Supply (UI)", supplyBefore.ui.toString());
  log.kv(
    "Mint Authority",
    mintInfo.mintAuthority?.toBase58() ?? "(disabled – cannot re-mint)",
  );
  log.blank();

  // ── Step 2: Resolve amount ───────────────────────────────────────────

  log.step(2, totalSteps, "Resolving burn amount…");

  let rawAmount: bigint;
  let uiAmount: number;

  if (isRaw) {
    rawAmount = BigInt(amountStr);
    if (rawAmount <= BigInt(0)) {
      log.fatal(`Invalid raw amount: "${amountStr}". Must be a positive integer.`);
    }
    uiAmount = fromRawAmount(rawAmount, decimals);
  } else {
    const parsed = parseFloat(amountStr);
    if (Number.isNaN(parsed) || parsed <= 0) {
      log.fatal(`Invalid amount: "${amountStr}". Must be a positive number.`);
    }
    uiAmount = parsed;
    rawAmount = toRawAmount(parsed, decimals);
  }

  log.kv("Burn Amount (UI)", `${uiAmount} ${cfg.tokenSymbol}`);
  log.kv("Burn Amount (raw)", rawAmount.toString());
  log.blank();

  // ── Step 3: Check owner's token balance ──────────────────────────────

  log.step(3, totalSteps, "Checking wallet balances…");

  // SOL balance for fees.
  const solBalance = await getSolBalance(connection, owner.publicKey);
  log.kv("Owner", owner.publicKey.toBase58());
  log.kv("SOL Balance", `${solBalance.toFixed(9)} SOL`);

  if (solBalance < 0.001) {
    log.warn(
      "Owner SOL balance is very low.  The transaction may fail due to " +
        "insufficient funds for fees.",
    );
  }

  // Check that the ATA exists.
  const ata = deriveATA(owner.publicKey, mint);
  const ataExistsFlag = await ataExists(connection, owner.publicKey, mint);

  if (!ataExistsFlag) {
    log.fatal(
      `No token account found for ${owner.publicKey.toBase58()} and ` +
        `mint ${mint.toBase58()}.\n` +
        "  You cannot burn tokens you don't hold.\n" +
        "  Mint tokens first: npx tsx src/mint-tokens.ts --amount <n>",
    );
  }

  log.kv("Token Account (ATA)", ata.toBase58());

  // Fetch current token balance.
  const tokenBalance = await getTokenBalance(connection, owner.publicKey, mint);

  if (!tokenBalance) {
    log.fatal(
      `Could not retrieve token balance for ${owner.publicKey.toBase58()}.\n` +
        "  The token account may not exist or the RPC may be unavailable.",
    );
  }

  log.kv(
    "Token Balance",
    `${tokenBalance.ui} ${cfg.tokenSymbol} (raw: ${tokenBalance.raw.toString()})`,
  );
  log.blank();

  // Validate that we have enough tokens to burn.
  if (tokenBalance.raw < rawAmount) {
    log.fatal(
      `Insufficient token balance to burn.\n` +
        `  Requested:  ${uiAmount} ${cfg.tokenSymbol} (raw: ${rawAmount.toString()})\n` +
        `  Available:  ${tokenBalance.ui} ${cfg.tokenSymbol} (raw: ${tokenBalance.raw.toString()})`,
    );
  }

  // ── Burn summary ─────────────────────────────────────────────────────

  const postBurnBalance = fromRawAmount(
    tokenBalance.raw - rawAmount,
    decimals,
  );
  const postBurnSupply = fromRawAmount(
    supplyBefore.raw - rawAmount,
    decimals,
  );

  log.step(4, totalSteps, "Burn summary");
  log.blank();

  log.kv("Mint", mint.toBase58());
  log.kv("Owner", owner.publicKey.toBase58());
  log.kv("Token Account", ata.toBase58());
  log.blank();
  log.kv("Amount to Burn (UI)", `${uiAmount} ${cfg.tokenSymbol}`);
  log.kv("Amount to Burn (raw)", rawAmount.toString());
  log.blank();
  log.kv(
    "Before → After Balance",
    `${tokenBalance.ui} → ${postBurnBalance} ${cfg.tokenSymbol}`,
  );
  log.kv(
    "Before → After Supply",
    `${supplyBefore.ui} → ${postBurnSupply} ${cfg.tokenSymbol}`,
  );
  log.blank();

  // Check if mint authority is revoked — warn that burned tokens are gone
  // forever.
  if (!mintInfo.mintAuthority) {
    log.warn(
      "The mint authority has been revoked.  Burned tokens can NEVER be\n" +
        "   re-minted.  This will permanently reduce the total supply.",
    );
    log.blank();
  } else {
    log.info(
      "The mint authority is still active.  Burned tokens could technically\n" +
        "   be re-minted later (assuming the authority is retained).",
    );
    log.blank();
  }

  log.warn(
    "Burning tokens is PERMANENT and IRREVERSIBLE.\n" +
      `   ${uiAmount} ${cfg.tokenSymbol} will be destroyed from your wallet.`,
  );
  log.blank();

  // ── Dry-run check ────────────────────────────────────────────────────

  if (dryRun) {
    log.warn("Dry-run mode – no transactions will be sent.");
    log.blank();
    log.info("The above burn would be executed.  Remove --dry-run to proceed.");
    log.blank();
    return;
  }

  // ── Interactive confirmation ──────────────────────────────────────────

  if (!skipConfirmation) {
    const confirmMessage =
      `Type "yes" to permanently burn ${uiAmount} ${cfg.tokenSymbol} ` +
      `from ${owner.publicKey.toBase58()}: `;

    const confirmed = await confirmPrompt(confirmMessage);

    if (!confirmed) {
      log.blank();
      log.info("Aborted.  No tokens were burned.");
      log.blank();
      return;
    }

    log.blank();
  }

  // ── Execute burn ─────────────────────────────────────────────────────

  log.step(5, totalSteps, "Sending burn transaction…");

  try {
    const { ata: burnedAta, signature } = await burnTokens(
      connection,
      owner,
      mint,
      rawAmount,
    );

    log.logTx("Tokens burned successfully", signature, cfg.cluster);
    log.kv("Token Account", burnedAta.toBase58());
    log.kv(
      "Amount Burned",
      `${uiAmount} ${cfg.tokenSymbol} (raw: ${rawAmount.toString()})`,
    );
    log.blank();
  } catch (err) {
    log.error(
      "Burn transaction failed:",
      err instanceof Error ? err.message : err,
    );
    log.blank();
    log.info(
      "The transaction may have failed due to network issues, " +
        "insufficient SOL for fees, or an invalid token account state.\n" +
        "  Please check the explorer and try again.",
    );
    process.exit(1);
  }

  // ── Post-burn verification ───────────────────────────────────────────

  log.info("Verifying post-burn state…");

  // Updated token balance.
  const postBalance = await getTokenBalance(connection, owner.publicKey, mint);

  if (postBalance) {
    log.kv(
      "New Token Balance",
      `${postBalance.ui} ${cfg.tokenSymbol} (raw: ${postBalance.raw.toString()})`,
    );
  } else {
    log.kv("New Token Balance", `0 ${cfg.tokenSymbol} (token account may have been closed)`);
  }

  // Updated supply.
  const supplyAfter = await getTokenSupply(connection, mint);
  log.kv(
    "New Total Supply",
    `${supplyAfter.ui} ${cfg.tokenSymbol} (raw: ${supplyAfter.raw.toString()})`,
  );

  // Verify the supply actually decreased.
  const expectedSupplyRaw = supplyBefore.raw - rawAmount;
  if (supplyAfter.raw !== expectedSupplyRaw) {
    log.warn(
      `Expected post-burn supply to be ${expectedSupplyRaw.toString()} ` +
        `but got ${supplyAfter.raw.toString()}.  This may indicate a ` +
        "confirmation delay — check the explorer.",
    );
  }

  // Verify the balance actually decreased.
  if (postBalance) {
    const expectedBalanceRaw = tokenBalance.raw - rawAmount;
    if (postBalance.raw !== expectedBalanceRaw) {
      log.warn(
        `Expected post-burn balance to be ${expectedBalanceRaw.toString()} ` +
          `but got ${postBalance.raw.toString()}.  This may indicate a ` +
          "confirmation delay — check the explorer.",
      );
    }
  }

  log.blank();

  // ── Done ─────────────────────────────────────────────────────────────

  log.divider();
  log.success("Burn complete!");
  log.blank();

  log.info(
    `${uiAmount} ${cfg.tokenSymbol} have been permanently destroyed.\n` +
      `  Total supply decreased from ${supplyBefore.ui} to ${supplyAfter.ui} ${cfg.tokenSymbol}.`,
  );
  log.blank();

  log.info("Related commands:");
  log.kv("Token info", "npx tsx src/token-info.ts");
  log.kv(
    "Verify ownership",
    "npx tsx src/verify-ownership.ts --wallet <pubkey>",
  );
  log.kv("Mint tokens", "npx tsx src/mint-tokens.ts --to <wallet> --amount <n>");
  log.kv("Holders snapshot", "npx tsx src/snapshot-holders.ts");
  log.kv("Revoke minting", "npx tsx src/revoke-mint.ts   (⚠ irreversible)");
  log.blank();
}

main().catch((err) => {
  log.error("Burn operation failed:", err);
  process.exit(1);
});
