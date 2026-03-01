/**
 * OpenTela Ownership Tools – Mint Tokens
 *
 * Mints SPL tokens from an existing mint to a specified wallet address.
 * The caller must be the current mint authority.  The destination's
 * Associated Token Account (ATA) is created automatically if it does not
 * already exist.
 *
 * Usage:
 *   npx tsx src/mint-tokens.ts --to <wallet> --amount <n>
 *
 * Options:
 *   --to <pubkey>       Recipient wallet public key (base58).
 *                        Defaults to the authority wallet itself.
 *   --amount <n>        Amount to mint in whole tokens (e.g. 1000).
 *   --mint <pubkey>     Override the TOKEN_MINT from .env.
 *   --decimals <n>      Override the TOKEN_DECIMALS from .env (only used
 *                        when the mint is not yet known on-chain and the
 *                        raw conversion needs a hint).
 *   --raw               Treat --amount as a raw integer (skip decimal
 *                        conversion).
 *   --dry-run           Print what would happen without sending txs.
 *
 * Environment / .env:
 *   TOKEN_MINT, TOKEN_DECIMALS, SOLANA_CLUSTER, KEYPAIR_PATH, etc.
 */

import { Command } from "commander";
import { PublicKey } from "@solana/web3.js";
import { loadConfig, getConnection, printConfig } from "./config.js";
import {
  mintTokensTo,
  toRawAmount,
  fromRawAmount,
  fetchMint,
  mintExists,
  deriveATA,
  ataExists,
  getTokenBalance,
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

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function main(): Promise<void> {
  const program = new Command()
    .name("mint-tokens")
    .description("Mint OpenTela SPL tokens to a wallet")
    .requiredOption("--amount <n>", "Amount to mint (whole tokens unless --raw)")
    .option("--to <pubkey>", "Recipient wallet public key")
    .option("--mint <pubkey>", "Token mint address (overrides TOKEN_MINT)")
    .option("--decimals <n>", "Token decimals override", parseInt)
    .option("--raw", "Treat amount as raw integer (no decimal conversion)")
    .option("--dry-run", "Print plan without sending transactions")
    .parse(process.argv);

  const opts = program.opts();

  // ── Configuration ────────────────────────────────────────────────────

  const cfg = loadConfig();
  if (cfg.debug) log.setDebug(true);

  const connection = getConnection(cfg);
  const payer = cfg.keypair;

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

  // Resolve recipient (default: the authority itself).
  const recipientStr = (opts.to as string | undefined) ?? payer.publicKey.toBase58();
  const recipient = parsePubkey(recipientStr, "recipient (--to)");

  // Resolve amount.
  const amountStr = opts.amount as string;
  const isRaw = opts.raw === true;
  const dryRun = opts.dryRun === true;

  // ── Validate mint exists ─────────────────────────────────────────────

  log.header("Mint OpenTela SPL Tokens");
  printConfig(cfg);
  log.blank();

  log.step(1, 4, "Validating mint account…");

  const exists = await mintExists(connection, mint);
  if (!exists) {
    log.fatal(
      `Mint ${mint.toBase58()} does not exist on-chain.\n` +
        "  Did you deploy it?  Run `npx tsx src/create-token.ts` first.",
    );
  }

  const mintInfo = await fetchMint(connection, mint);
  const decimals = (opts.decimals as number | undefined) ?? mintInfo.decimals;

  log.success("Mint exists on-chain");
  log.kv("Mint", mint.toBase58());
  log.kv("Decimals", decimals);
  log.kv("Current Supply", fromRawAmount(mintInfo.supply, decimals).toString());
  log.kv(
    "Mint Authority",
    mintInfo.mintAuthority?.toBase58() ?? "(disabled – no further minting possible)",
  );
  log.blank();

  // Check that payer is the mint authority.
  if (!mintInfo.mintAuthority) {
    log.fatal(
      "The mint authority has been revoked – no further tokens can be minted.\n" +
        "  This is irreversible.",
    );
  }
  if (!mintInfo.mintAuthority.equals(payer.publicKey)) {
    log.fatal(
      `Your keypair (${payer.publicKey.toBase58()}) is not the mint authority.\n` +
        `  Current authority: ${mintInfo.mintAuthority.toBase58()}`,
    );
  }

  // ── Resolve amount ───────────────────────────────────────────────────

  let rawAmount: bigint;
  let uiAmount: number;

  if (isRaw) {
    rawAmount = BigInt(amountStr);
    uiAmount = fromRawAmount(rawAmount, decimals);
  } else {
    const parsed = parseFloat(amountStr);
    if (Number.isNaN(parsed) || parsed <= 0) {
      log.fatal(`Invalid amount: "${amountStr}". Must be a positive number.`);
    }
    uiAmount = parsed;
    rawAmount = toRawAmount(parsed, decimals);
  }

  log.step(2, 4, "Computing mint parameters…");
  log.kv("Recipient", recipient.toBase58());
  log.kv("Amount (UI)", `${uiAmount} ${cfg.tokenSymbol}`);
  log.kv("Amount (raw)", rawAmount.toString());
  log.blank();

  // ── Check SOL balance of payer ───────────────────────────────────────

  log.step(3, 4, "Checking payer SOL balance…");
  const solBalance = await getSolBalance(connection, payer.publicKey);
  log.kv("Payer SOL", `${solBalance.toFixed(9)} SOL`);

  if (solBalance < 0.001) {
    log.warn(
      "Payer SOL balance is very low.  The transaction may fail due to " +
        "insufficient funds for fees and rent.",
    );
  }
  log.blank();

  // ── Check if recipient ATA already exists ────────────────────────────

  const recipientAta = deriveATA(recipient, mint);
  const ataAlreadyExists = await ataExists(connection, recipient, mint);
  log.debug(`Recipient ATA: ${recipientAta.toBase58()}`);
  log.debug(`ATA exists: ${ataAlreadyExists}`);

  if (!ataAlreadyExists) {
    log.info(
      `Recipient ATA does not exist yet – it will be created automatically ` +
        `(rent paid by payer).`,
    );
    log.blank();
  }

  // ── Dry-run check ────────────────────────────────────────────────────

  if (dryRun) {
    log.warn("Dry-run mode – no transactions will be sent.");
    log.blank();
    log.info("The following would be executed:");
    if (!ataAlreadyExists) {
      log.step(1, 2, `Create ATA ${recipientAta.toBase58()} for ${recipient.toBase58()}`);
      log.step(2, 2, `Mint ${uiAmount} ${cfg.tokenSymbol} to ATA`);
    } else {
      log.step(1, 1, `Mint ${uiAmount} ${cfg.tokenSymbol} to ATA ${recipientAta.toBase58()}`);
    }
    log.blank();
    return;
  }

  // ── Execute mint ─────────────────────────────────────────────────────

  log.step(4, 4, "Sending mint transaction…");

  const { ata, signature } = await mintTokensTo(
    connection,
    payer,
    mint,
    recipient,
    rawAmount,
  );

  log.logTx("Tokens minted successfully", signature, cfg.cluster);
  log.kv("Destination ATA", ata.toBase58());
  log.kv("Amount", `${uiAmount} ${cfg.tokenSymbol} (raw: ${rawAmount.toString()})`);
  log.blank();

  // ── Post-mint verification ───────────────────────────────────────────

  log.info("Verifying post-mint balance…");

  const balance = await getTokenBalance(connection, recipient, mint);
  if (balance) {
    log.kv("Recipient Balance", `${balance.ui} ${cfg.tokenSymbol} (raw: ${balance.raw.toString()})`);
  } else {
    log.warn("Could not retrieve post-mint balance (ATA may not have settled yet).");
  }

  // Updated supply.
  const updatedMint = await fetchMint(connection, mint);
  log.kv("New Total Supply", fromRawAmount(updatedMint.supply, decimals).toString());
  log.blank();

  // ── Done ─────────────────────────────────────────────────────────────

  log.divider();
  log.success("Minting complete!");
  log.blank();
  log.info("Next steps:");
  log.kv("Check balance", `npx tsx src/token-info.ts`);
  log.kv("Verify ownership", `npx tsx src/verify-ownership.ts --wallet ${recipient.toBase58()}`);
  log.kv("Transfer tokens", `npx tsx src/transfer-tokens.ts --to <wallet> --amount <n>`);
  log.blank();
}

main().catch((err) => {
  log.error("Failed to mint tokens:", err);
  process.exit(1);
});
