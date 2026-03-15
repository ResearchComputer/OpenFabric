/**
 * OpenTela Ownership Tools – Revoke Mint Authority
 *
 * Permanently disables the mint authority on an SPL token, making it
 * impossible to ever mint additional tokens.  This is an **irreversible**
 * operation and should only be performed when the total supply is final.
 *
 * Optionally, this script can also revoke the freeze authority at the
 * same time (--revoke-freeze).
 *
 * Usage:
 *   npx tsx src/revoke-mint.ts
 *   npx tsx src/revoke-mint.ts --mint <pubkey>
 *   npx tsx src/revoke-mint.ts --revoke-freeze
 *   npx tsx src/revoke-mint.ts --dry-run
 *
 * Options:
 *   --mint <pubkey>     Token mint address (overrides TOKEN_MINT from .env).
 *   --revoke-freeze     Also revoke the freeze authority (irreversible).
 *   --yes               Skip the interactive confirmation prompt.
 *   --dry-run           Print what would happen without sending transactions.
 *
 * Environment / .env:
 *   TOKEN_MINT, SOLANA_CLUSTER, KEYPAIR_PATH, etc.
 *
 * ⚠  WARNING: Revoking the mint authority is PERMANENT.  No new tokens
 *    can ever be created for this mint after this operation completes.
 *    Make sure the total supply is correct before proceeding.
 */

import { Command } from "commander";
import { PublicKey } from "@solana/web3.js";
import { createInterface } from "node:readline";
import { loadConfig, getConnection, printConfig } from "./config.js";
import {
  fetchMint,
  mintExists,
  fromRawAmount,
  revokeMintAuthority,
  revokeFreezeAuthority,
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
    .name("revoke-mint")
    .description(
      "Permanently revoke the mint authority on an OpenTela SPL token (⚠ irreversible)",
    )
    .option("--mint <pubkey>", "Token mint address (overrides TOKEN_MINT)")
    .option("--revoke-freeze", "Also revoke the freeze authority")
    .option("--yes", "Skip interactive confirmation prompt")
    .option("--dry-run", "Print plan without sending transactions")
    .parse(process.argv);

  const opts = program.opts();

  // ── Configuration ────────────────────────────────────────────────────

  const cfg = loadConfig();
  if (cfg.debug) log.setDebug(true);

  const connection = getConnection(cfg);
  const authority = cfg.keypair;
  const alsoRevokeFreezeFlag = opts.revokeFreeze === true;
  const skipConfirmation = opts.yes === true;
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

  // ── Print header ─────────────────────────────────────────────────────

  log.header("⚠  Revoke Mint Authority (Irreversible)");
  printConfig(cfg);
  log.blank();

  // ── Validate mint exists ─────────────────────────────────────────────

  const totalSteps = alsoRevokeFreezeFlag ? 5 : 4;

  log.step(1, totalSteps, "Validating mint account…");

  const exists = await mintExists(connection, mint);
  if (!exists) {
    log.fatal(
      `Mint ${mint.toBase58()} does not exist on-chain.\n` +
        "  Check the TOKEN_MINT value in .env or pass --mint with a valid address.",
    );
  }

  const mintInfo = await fetchMint(connection, mint);
  const supply = await getTokenSupply(connection, mint);

  log.success("Mint exists on-chain");
  log.kv("Mint", mint.toBase58());
  log.kv("Explorer", log.explorerAddressUrl(mint.toBase58(), cfg.cluster));
  log.kv("Decimals", mintInfo.decimals);
  log.kv("Total Supply (raw)", supply.raw.toString());
  log.kv("Total Supply (UI)", supply.ui.toString());
  log.blank();

  // ── Check current authorities ────────────────────────────────────────

  log.step(2, totalSteps, "Checking current authorities…");

  const currentMintAuthority = mintInfo.mintAuthority;
  const currentFreezeAuthority = mintInfo.freezeAuthority;

  log.kv(
    "Mint Authority",
    currentMintAuthority?.toBase58() ?? "(already disabled)",
  );
  log.kv(
    "Freeze Authority",
    currentFreezeAuthority?.toBase58() ?? "(already disabled)",
  );
  log.blank();

  // Check if there's anything to revoke.
  if (!currentMintAuthority && !alsoRevokeFreezeFlag) {
    log.warn("The mint authority is already disabled.  Nothing to do.");
    log.blank();
    return;
  }

  if (!currentMintAuthority && alsoRevokeFreezeFlag && !currentFreezeAuthority) {
    log.warn(
      "Both the mint authority and freeze authority are already disabled.  " +
        "Nothing to do.",
    );
    log.blank();
    return;
  }

  // Verify the caller is the current authority.
  if (currentMintAuthority && !currentMintAuthority.equals(authority.publicKey)) {
    log.fatal(
      `Your keypair (${authority.publicKey.toBase58()}) is not the current mint authority.\n` +
        `  Current mint authority: ${currentMintAuthority.toBase58()}\n` +
        "  You must use the keypair that holds the mint authority to revoke it.",
    );
  }

  if (
    alsoRevokeFreezeFlag &&
    currentFreezeAuthority &&
    !currentFreezeAuthority.equals(authority.publicKey)
  ) {
    log.fatal(
      `Your keypair (${authority.publicKey.toBase58()}) is not the current freeze authority.\n` +
        `  Current freeze authority: ${currentFreezeAuthority.toBase58()}\n` +
        "  You must use the keypair that holds the freeze authority to revoke it.",
    );
  }

  // ── SOL balance check ────────────────────────────────────────────────

  const solBalance = await getSolBalance(connection, authority.publicKey);
  log.kv("Authority SOL", `${solBalance.toFixed(9)} SOL`);

  if (solBalance < 0.001) {
    log.warn(
      "Authority SOL balance is very low.  The transaction may fail due to " +
        "insufficient funds for fees.",
    );
  }
  log.blank();

  // ── Summary of what will happen ──────────────────────────────────────

  log.step(3, totalSteps, "Revocation plan");
  log.blank();

  const actions: string[] = [];

  if (currentMintAuthority) {
    actions.push(
      `Revoke MINT authority on ${mint.toBase58()} ` +
        `(current: ${currentMintAuthority.toBase58()})`,
    );
  }

  if (alsoRevokeFreezeFlag && currentFreezeAuthority) {
    actions.push(
      `Revoke FREEZE authority on ${mint.toBase58()} ` +
        `(current: ${currentFreezeAuthority.toBase58()})`,
    );
  }

  for (let i = 0; i < actions.length; i++) {
    log.step(i + 1, actions.length, actions[i]!);
  }
  log.blank();

  log.warn(
    "This operation is PERMANENT and IRREVERSIBLE.\n" +
      "   After revoking the mint authority, no new tokens can ever be\n" +
      "   created for this mint.  The total supply will be locked at\n" +
      `   ${supply.ui} ${cfg.tokenSymbol} forever.`,
  );
  log.blank();

  // ── Dry-run check ────────────────────────────────────────────────────

  if (dryRun) {
    log.warn("Dry-run mode – no transactions will be sent.");
    log.blank();
    log.info("The above actions would be executed.  Remove --dry-run to proceed.");
    log.blank();
    return;
  }

  // ── Interactive confirmation ──────────────────────────────────────────

  if (!skipConfirmation) {
    log.blank();

    const confirmMessage = alsoRevokeFreezeFlag
      ? `Type "yes" to permanently revoke BOTH the mint and freeze authorities: `
      : `Type "yes" to permanently revoke the mint authority: `;

    const confirmed = await confirmPrompt(confirmMessage);

    if (!confirmed) {
      log.blank();
      log.info("Aborted.  No changes were made.");
      log.blank();
      return;
    }

    log.blank();
  }

  // ── Execute revocations ──────────────────────────────────────────────

  const revokeStep = alsoRevokeFreezeFlag ? 4 : 4;

  // Revoke mint authority.
  if (currentMintAuthority) {
    log.step(revokeStep, totalSteps, "Revoking mint authority…");

    try {
      const signature = await revokeMintAuthority(connection, authority, mint);

      log.logTx("Mint authority revoked", signature, cfg.cluster);
      log.kv("Mint", mint.toBase58());
      log.kv("Previous Authority", currentMintAuthority.toBase58());
      log.kv("New Authority", "(disabled – no further minting possible)");
      log.blank();
    } catch (err) {
      log.error(
        "Failed to revoke mint authority:",
        err instanceof Error ? err.message : err,
      );
      log.blank();
      log.info(
        "The transaction may have failed due to network issues or " +
          "insufficient SOL.  Please try again.",
      );
      process.exit(1);
    }
  }

  // Revoke freeze authority (optional).
  if (alsoRevokeFreezeFlag && currentFreezeAuthority) {
    log.step(5, totalSteps, "Revoking freeze authority…");

    try {
      const signature = await revokeFreezeAuthority(
        connection,
        authority,
        mint,
      );

      log.logTx("Freeze authority revoked", signature, cfg.cluster);
      log.kv("Mint", mint.toBase58());
      log.kv("Previous Freeze Authority", currentFreezeAuthority.toBase58());
      log.kv("New Freeze Authority", "(disabled – no further freezing possible)");
      log.blank();
    } catch (err) {
      log.error(
        "Failed to revoke freeze authority:",
        err instanceof Error ? err.message : err,
      );
      log.blank();
      log.warn(
        "The mint authority was revoked successfully, but the freeze " +
          "authority revocation failed.  You can retry with:\n" +
          "  npx tsx src/revoke-mint.ts --revoke-freeze",
      );
      process.exit(1);
    }
  }

  // ── Post-revocation verification ─────────────────────────────────────

  log.info("Verifying revocation…");

  const updatedMint = await fetchMint(connection, mint);

  const mintAuthorityStatus = updatedMint.mintAuthority
    ? `⚠ still set: ${updatedMint.mintAuthority.toBase58()}`
    : "✔ disabled (no further minting possible)";

  const freezeAuthorityStatus = updatedMint.freezeAuthority
    ? alsoRevokeFreezeFlag
      ? `⚠ still set: ${updatedMint.freezeAuthority.toBase58()}`
      : updatedMint.freezeAuthority.toBase58()
    : "✔ disabled (no further freezing possible)";

  log.kv("Mint Authority", mintAuthorityStatus);
  log.kv("Freeze Authority", freezeAuthorityStatus);
  log.kv(
    "Final Supply",
    `${fromRawAmount(updatedMint.supply, updatedMint.decimals)} ${cfg.tokenSymbol} ` +
      `(raw: ${updatedMint.supply.toString()})`,
  );
  log.blank();

  // Warn if something unexpected happened.
  if (currentMintAuthority && updatedMint.mintAuthority) {
    log.error(
      "Mint authority does not appear to have been revoked!  " +
        "The transaction may not have been confirmed.  " +
        "Please check the explorer and try again.",
    );
    log.blank();
    process.exit(1);
  }

  if (
    alsoRevokeFreezeFlag &&
    currentFreezeAuthority &&
    updatedMint.freezeAuthority
  ) {
    log.error(
      "Freeze authority does not appear to have been revoked!  " +
        "The transaction may not have been confirmed.  " +
        "Please check the explorer and try again.",
    );
    log.blank();
    process.exit(1);
  }

  // ── Done ─────────────────────────────────────────────────────────────

  log.divider();
  log.success("Authority revocation complete!");
  log.blank();

  if (currentMintAuthority) {
    log.info(
      `The mint authority for ${mint.toBase58()} has been permanently disabled.\n` +
        `  No new ${cfg.tokenSymbol} tokens can ever be created.\n` +
        `  The total supply is permanently locked at ` +
        `${fromRawAmount(updatedMint.supply, updatedMint.decimals)} ${cfg.tokenSymbol}.`,
    );
    log.blank();
  }

  if (alsoRevokeFreezeFlag && currentFreezeAuthority) {
    log.info(
      `The freeze authority for ${mint.toBase58()} has been permanently disabled.\n` +
        "  Token accounts can no longer be frozen or thawed by any party.",
    );
    log.blank();
  }

  log.info("Related commands:");
  log.kv("Token info", "npx tsx src/token-info.ts");
  log.kv(
    "Verify ownership",
    "npx tsx src/verify-ownership.ts --wallet <pubkey>",
  );
  log.kv("Holders snapshot", "npx tsx src/snapshot-holders.ts");
  log.blank();
}

main().catch((err) => {
  log.error("Revocation failed:", err);
  process.exit(1);
});
