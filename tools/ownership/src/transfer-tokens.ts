/**
 * OpenTela Ownership Tools – Transfer Tokens
 *
 * Transfers SPL tokens from the authority wallet to a specified recipient.
 * The destination's Associated Token Account (ATA) is created automatically
 * if it does not already exist (rent paid by the sender).
 *
 * Usage:
 *   npx tsx src/transfer-tokens.ts --to <wallet> --amount <n>
 *
 * Options:
 *   --to <pubkey>       Recipient wallet public key (base58).  Required.
 *   --amount <n>        Amount to transfer in whole tokens (e.g. 50.5).
 *   --mint <pubkey>     Override the TOKEN_MINT from .env.
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
  transferTokens,
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
    .name("transfer-tokens")
    .description("Transfer OpenTela SPL tokens to another wallet")
    .requiredOption("--to <pubkey>", "Recipient wallet public key (base58)")
    .requiredOption(
      "--amount <n>",
      "Amount to transfer (whole tokens unless --raw)",
    )
    .option("--mint <pubkey>", "Token mint address (overrides TOKEN_MINT)")
    .option("--raw", "Treat amount as raw integer (no decimal conversion)")
    .option("--dry-run", "Print plan without sending transactions")
    .parse(process.argv);

  const opts = program.opts();

  // ── Configuration ────────────────────────────────────────────────────

  const cfg = loadConfig();
  if (cfg.debug) log.setDebug(true);

  const connection = getConnection(cfg);
  const sender = cfg.keypair;

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

  // Resolve recipient.
  const recipientStr = opts.to as string;
  const recipient = parsePubkey(recipientStr, "recipient (--to)");

  // Prevent self-transfers (they succeed on-chain but are wasteful).
  if (recipient.equals(sender.publicKey)) {
    log.warn(
      "Recipient is the same as the sender.  This is a no-op transfer " +
        "(you'd only pay a transaction fee).",
    );
  }

  // Resolve amount.
  const amountStr = opts.amount as string;
  const isRaw = opts.raw === true;
  const dryRun = opts.dryRun === true;

  // ── Validate mint ────────────────────────────────────────────────────

  log.header("Transfer OpenTela SPL Tokens");
  printConfig(cfg);
  log.blank();

  log.step(1, 5, "Validating mint account…");

  const exists = await mintExists(connection, mint);
  if (!exists) {
    log.fatal(
      `Mint ${mint.toBase58()} does not exist on-chain.\n` +
        "  Did you deploy it?  Run `npx tsx src/create-token.ts` first.",
    );
  }

  const mintInfo = await fetchMint(connection, mint);
  const decimals = mintInfo.decimals;

  log.success("Mint exists on-chain");
  log.kv("Mint", mint.toBase58());
  log.kv("Decimals", decimals);
  log.kv(
    "Total Supply",
    fromRawAmount(mintInfo.supply, decimals).toString(),
  );
  log.blank();

  // ── Resolve amount ───────────────────────────────────────────────────

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

  // ── Check sender balance ─────────────────────────────────────────────

  log.step(2, 5, "Checking sender balances…");

  const solBalance = await getSolBalance(connection, sender.publicKey);
  log.kv("Sender", sender.publicKey.toBase58());
  log.kv("SOL Balance", `${solBalance.toFixed(9)} SOL`);

  if (solBalance < 0.001) {
    log.warn(
      "Sender SOL balance is very low.  The transaction may fail due to " +
        "insufficient funds for fees.",
    );
  }

  const senderTokenBalance = await getTokenBalance(
    connection,
    sender.publicKey,
    mint,
  );

  if (!senderTokenBalance) {
    log.fatal(
      `Sender ${sender.publicKey.toBase58()} has no token account for ` +
        `mint ${mint.toBase58()}.  There are no tokens to transfer.\n` +
        "  Mint tokens first: npx tsx src/mint-tokens.ts --amount <n>",
    );
  }

  log.kv(
    "Token Balance",
    `${senderTokenBalance.ui} ${cfg.tokenSymbol} (raw: ${senderTokenBalance.raw.toString()})`,
  );

  if (senderTokenBalance.raw < rawAmount) {
    log.fatal(
      `Insufficient token balance.\n` +
        `  Required: ${uiAmount} ${cfg.tokenSymbol} (raw: ${rawAmount.toString()})\n` +
        `  Available: ${senderTokenBalance.ui} ${cfg.tokenSymbol} (raw: ${senderTokenBalance.raw.toString()})`,
    );
  }
  log.blank();

  // ── Check recipient ATA ──────────────────────────────────────────────

  log.step(3, 5, "Resolving recipient token account…");

  const recipientAta = deriveATA(recipient, mint);
  const recipientAtaExists = await ataExists(connection, recipient, mint);

  log.kv("Recipient", recipient.toBase58());
  log.kv("Recipient ATA", recipientAta.toBase58());
  log.kv("ATA Exists", recipientAtaExists ? "yes" : "no (will be created)");

  if (!recipientAtaExists) {
    log.info(
      "The recipient's Associated Token Account does not exist yet.  " +
        "It will be created automatically as part of this transaction " +
        "(rent funded by sender).",
    );
  }
  log.blank();

  // ── Transfer summary ─────────────────────────────────────────────────

  log.step(4, 5, "Transfer summary");
  log.blank();
  log.kv("From", sender.publicKey.toBase58());
  log.kv("To", recipient.toBase58());
  log.kv("Mint", mint.toBase58());
  log.kv("Amount (UI)", `${uiAmount} ${cfg.tokenSymbol}`);
  log.kv("Amount (raw)", rawAmount.toString());
  log.blank();

  // ── Dry-run check ────────────────────────────────────────────────────

  if (dryRun) {
    log.warn("Dry-run mode – no transactions will be sent.");
    log.blank();
    log.info("The following would be executed:");
    const steps: string[] = [];
    if (!recipientAtaExists) {
      steps.push(
        `Create ATA ${recipientAta.toBase58()} for ${recipient.toBase58()}`,
      );
    }
    steps.push(
      `Transfer ${uiAmount} ${cfg.tokenSymbol} from ` +
        `${sender.publicKey.toBase58()} → ${recipient.toBase58()}`,
    );
    steps.forEach((msg, i) => log.step(i + 1, steps.length, msg));
    log.blank();
    return;
  }

  // ── Execute transfer ─────────────────────────────────────────────────

  log.step(5, 5, "Sending transfer transaction…");

  const { sourceAta, destAta, signature } = await transferTokens(
    connection,
    sender,
    mint,
    recipient,
    rawAmount,
  );

  log.logTx("Tokens transferred successfully", signature, cfg.cluster);
  log.kv("Source ATA", sourceAta.toBase58());
  log.kv("Dest ATA", destAta.toBase58());
  log.kv(
    "Amount",
    `${uiAmount} ${cfg.tokenSymbol} (raw: ${rawAmount.toString()})`,
  );
  log.blank();

  // ── Post-transfer verification ───────────────────────────────────────

  log.info("Verifying post-transfer balances…");

  const senderPostBalance = await getTokenBalance(
    connection,
    sender.publicKey,
    mint,
  );
  const recipientPostBalance = await getTokenBalance(
    connection,
    recipient,
    mint,
  );

  if (senderPostBalance) {
    log.kv(
      "Sender Balance",
      `${senderPostBalance.ui} ${cfg.tokenSymbol} (raw: ${senderPostBalance.raw.toString()})`,
    );
  } else {
    log.kv("Sender Balance", "0 (token account closed)");
  }

  if (recipientPostBalance) {
    log.kv(
      "Recipient Balance",
      `${recipientPostBalance.ui} ${cfg.tokenSymbol} (raw: ${recipientPostBalance.raw.toString()})`,
    );
  } else {
    log.warn("Could not retrieve recipient post-transfer balance.");
  }
  log.blank();

  // ── Done ─────────────────────────────────────────────────────────────

  log.divider();
  log.success("Transfer complete!");
  log.blank();
  log.info("Next steps:");
  log.kv(
    "Verify ownership",
    `npx tsx src/verify-ownership.ts --wallet ${recipient.toBase58()}`,
  );
  log.kv("Check token info", "npx tsx src/token-info.ts");
  log.kv(
    "View on explorer",
    log.explorerTxUrl(signature, cfg.cluster),
  );
  log.blank();
}

main().catch((err) => {
  log.error("Failed to transfer tokens:", err);
  process.exit(1);
});
