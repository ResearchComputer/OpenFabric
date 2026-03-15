/**
 * OpenTela Ownership Tools – Create Token
 *
 * Creates a new SPL token mint on Solana with the configured metadata
 * properties (name, symbol, decimals, URI).  Optionally mints an initial
 * supply to the authority wallet immediately after creation.
 *
 * Usage:
 *   npx tsx src/create-token.ts [options]
 *
 * Options are read from the environment / .env file:
 *   TOKEN_NAME, TOKEN_SYMBOL, TOKEN_DECIMALS, TOKEN_URI,
 *   TOKEN_INITIAL_SUPPLY, SOLANA_CLUSTER, KEYPAIR_PATH, etc.
 *
 * You can also pass overrides via CLI flags:
 *   --name <name>       Token name
 *   --symbol <symbol>   Token symbol / ticker
 *   --decimals <n>      Decimal places (default 9)
 *   --uri <uri>         Off-chain metadata URI
 *   --supply <n>        Initial supply to mint (whole tokens)
 *   --no-freeze         Do not set a freeze authority
 *   --dry-run           Print what would happen without sending txs
 */

import { Command } from "commander";
import {
  PublicKey,
  Transaction,
  TransactionInstruction,
  sendAndConfirmTransaction,
} from "@solana/web3.js";
import { loadConfig, getConnection, printConfig } from "./config.js";
import { createSPLToken, mintTokensTo, toRawAmount } from "./spl-helpers.js";
import * as log from "./log.js";

// ---------------------------------------------------------------------------
// Metaplex Token Metadata – constants & manual instruction builder
// ---------------------------------------------------------------------------

/**
 * The canonical Metaplex Token Metadata program ID.
 */
const TOKEN_METADATA_PROGRAM_ID = new PublicKey(
  "metaqbxxUerdq28cj1RbAWkYQm3ybzjb6a8bt518x1s",
);

/**
 * Derive the Metadata PDA for a given mint.
 */
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
 * Encode a Borsh string: 4-byte LE length prefix followed by UTF-8 bytes.
 */
function borshString(value: string): Buffer {
  const encoded = Buffer.from(value, "utf-8");
  const lenBuf = Buffer.alloc(4);
  lenBuf.writeUInt32LE(encoded.length, 0);
  return Buffer.concat([lenBuf, encoded]);
}

/**
 * Build the raw instruction data for CreateMetadataAccountV3.
 *
 * The instruction discriminator for CreateMetadataAccountV3 is 33 (single
 * byte).  The data payload is Borsh-encoded with the following structure:
 *
 *   discriminator:        u8  = 33
 *   name:                 String (4-byte len + UTF-8)
 *   symbol:               String
 *   uri:                  String
 *   seller_fee_bps:       u16 LE
 *   creators:             Option<Vec<Creator>>  — we set None (0x00)
 *   collection:           Option<Collection>    — we set None (0x00)
 *   uses:                 Option<Uses>          — we set None (0x00)
 *   is_mutable:           bool (u8)
 *   collection_details:   Option<CollectionDetails> — we set None (0x00)
 */
function buildCreateMetadataV3InstructionData(
  name: string,
  symbol: string,
  uri: string,
  isMutable: boolean,
): Buffer {
  const parts: Buffer[] = [];

  // Discriminator: CreateMetadataAccountV3 = 33
  parts.push(Buffer.from([33]));

  // --- CreateMetadataAccountArgsV3 ---

  // DataV2
  parts.push(borshString(name));
  parts.push(borshString(symbol));
  parts.push(borshString(uri));

  // seller_fee_basis_points: u16 LE = 0
  const feeBuf = Buffer.alloc(2);
  feeBuf.writeUInt16LE(0, 0);
  parts.push(feeBuf);

  // creators: Option<Vec<Creator>> = None
  parts.push(Buffer.from([0]));

  // collection: Option<Collection> = None
  parts.push(Buffer.from([0]));

  // uses: Option<Uses> = None
  parts.push(Buffer.from([0]));

  // is_mutable: bool
  parts.push(Buffer.from([isMutable ? 1 : 0]));

  // collection_details: Option<CollectionDetails> = None
  parts.push(Buffer.from([0]));

  return Buffer.concat(parts);
}

/**
 * Build a CreateMetadataAccountV3 instruction compatible with
 * `@solana/web3.js` Transaction without pulling in the Umi runtime.
 *
 * Accounts layout (from the on-chain program):
 *   0. [writable]        metadata PDA
 *   1. []                mint
 *   2. [signer]          mint authority
 *   3. [signer, writable] payer
 *   4. []                update authority
 *   5. []                system program
 *   6. []                rent sysvar (optional but historically expected)
 */
function createMetadataAccountV3Instruction(args: {
  metadata: PublicKey;
  mint: PublicKey;
  mintAuthority: PublicKey;
  payer: PublicKey;
  updateAuthority: PublicKey;
  name: string;
  symbol: string;
  uri: string;
  isMutable: boolean;
}): TransactionInstruction {
  const data = buildCreateMetadataV3InstructionData(
    args.name,
    args.symbol,
    args.uri,
    args.isMutable,
  );

  const keys = [
    { pubkey: args.metadata, isSigner: false, isWritable: true },
    { pubkey: args.mint, isSigner: false, isWritable: false },
    { pubkey: args.mintAuthority, isSigner: true, isWritable: false },
    { pubkey: args.payer, isSigner: true, isWritable: true },
    { pubkey: args.updateAuthority, isSigner: false, isWritable: false },
    {
      pubkey: new PublicKey("11111111111111111111111111111111"),
      isSigner: false,
      isWritable: false,
    },
    // Rent sysvar — not strictly required on modern validators but some
    // older RPC nodes still expect it in the account list.
    {
      pubkey: new PublicKey("SysvarRent111111111111111111111111111111111"),
      isSigner: false,
      isWritable: false,
    },
  ];

  return new TransactionInstruction({
    programId: TOKEN_METADATA_PROGRAM_ID,
    keys,
    data,
  });
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function main(): Promise<void> {
  const program = new Command()
    .name("create-token")
    .description("Create a new OpenTela SPL ownership token")
    .option("--name <name>", "Token name")
    .option("--symbol <symbol>", "Token symbol / ticker")
    .option("--decimals <n>", "Decimal places", parseInt)
    .option("--uri <uri>", "Off-chain metadata URI")
    .option("--supply <n>", "Initial supply to mint (whole tokens)", parseFloat)
    .option("--no-freeze", "Do not set a freeze authority")
    .option("--dry-run", "Print plan without sending transactions")
    .parse(process.argv);

  const opts = program.opts();

  // Load configuration (env + .env file).
  const cfg = loadConfig();
  if (cfg.debug) log.setDebug(true);

  // CLI flags override env vars.
  const tokenName = (opts.name as string | undefined) ?? cfg.tokenName;
  const tokenSymbol = (opts.symbol as string | undefined) ?? cfg.tokenSymbol;
  const tokenDecimals =
    (opts.decimals as number | undefined) ?? cfg.tokenDecimals;
  const tokenUri = (opts.uri as string | undefined) ?? cfg.tokenUri;
  const initialSupply =
    (opts.supply as number | undefined) ?? cfg.tokenInitialSupply;
  const noFreeze = opts.freeze === false; // commander uses --no-freeze → freeze=false
  const dryRun = opts.dryRun === true;

  const connection = getConnection(cfg);
  const payer = cfg.keypair;

  // Print summary.
  log.header("Create OpenTela SPL Token");
  printConfig(cfg);
  log.blank();
  log.kv("Token Name", tokenName);
  log.kv("Token Symbol", tokenSymbol);
  log.kv("Token Decimals", tokenDecimals);
  log.kv("Token URI", tokenUri || "(none)");
  log.kv(
    "Freeze Authority",
    noFreeze ? "disabled" : payer.publicKey.toBase58(),
  );
  log.kv(
    "Initial Supply",
    initialSupply > 0 ? `${initialSupply} ${tokenSymbol}` : "(none)",
  );
  log.kv("Dry Run", dryRun);
  log.blank();

  if (dryRun) {
    log.warn("Dry-run mode – no transactions will be sent.");
    log.blank();
    log.info("The following steps would be executed:");
    log.step(1, initialSupply > 0 ? 3 : 2, "Create SPL Token mint account");
    log.step(
      2,
      initialSupply > 0 ? 3 : 2,
      "Attach on-chain metadata (Metaplex Token Metadata)",
    );
    if (initialSupply > 0) {
      log.step(
        3,
        3,
        `Mint ${initialSupply} ${tokenSymbol} to ${payer.publicKey.toBase58()}`,
      );
    }
    log.blank();
    return;
  }

  // ── Step 1: Create the mint ──────────────────────────────────────────

  const totalSteps = 2 + (initialSupply > 0 ? 1 : 0);

  log.step(1, totalSteps, "Creating SPL Token mint…");

  const freezeAuthority = noFreeze ? null : payer.publicKey;
  const {
    mint,
    mintKeypair,
    signature: createSig,
  } = await createSPLToken(
    connection,
    payer,
    tokenDecimals,
    undefined, // generate fresh keypair
    freezeAuthority,
  );

  log.logTx("Mint created", createSig, cfg.cluster);
  log.kv("Mint Address", mint.toBase58());
  log.kv("Explorer", log.explorerAddressUrl(mint.toBase58(), cfg.cluster));
  log.blank();

  // ── Step 2: Attach Metaplex metadata ─────────────────────────────────

  log.step(2, totalSteps, "Attaching on-chain metadata…");

  const metadataPDA = deriveMetadataPDA(mint);
  log.debug(`Metadata PDA: ${metadataPDA.toBase58()}`);

  const metadataIx = createMetadataAccountV3Instruction({
    metadata: metadataPDA,
    mint,
    mintAuthority: payer.publicKey,
    payer: payer.publicKey,
    updateAuthority: payer.publicKey,
    name: tokenName,
    symbol: tokenSymbol,
    uri: tokenUri,
    isMutable: true,
  });

  const metadataTx = new Transaction().add(metadataIx);
  const metadataSig = await sendAndConfirmTransaction(
    connection,
    metadataTx,
    [payer],
    { commitment: "confirmed" },
  );

  log.logTx("Metadata attached", metadataSig, cfg.cluster);
  log.kv("Metadata PDA", metadataPDA.toBase58());
  log.blank();

  // ── Step 3 (optional): Mint initial supply ───────────────────────────

  if (initialSupply > 0) {
    log.step(
      3,
      totalSteps,
      `Minting ${initialSupply} ${tokenSymbol} to authority wallet…`,
    );

    const rawAmount = toRawAmount(initialSupply, tokenDecimals);
    const { ata, signature: mintSig } = await mintTokensTo(
      connection,
      payer,
      mint,
      payer.publicKey,
      rawAmount,
    );

    log.logTx("Tokens minted", mintSig, cfg.cluster);
    log.kv("Destination ATA", ata.toBase58());
    log.kv(
      "Amount",
      `${initialSupply} ${tokenSymbol} (raw: ${rawAmount.toString()})`,
    );
    log.blank();
  }

  // ── Done ─────────────────────────────────────────────────────────────

  log.divider();
  log.success("Token created successfully!");
  log.blank();
  log.info("Add this to your .env file to use the token with other scripts:");
  log.blank();
  console.log(`  TOKEN_MINT=${mint.toBase58()}`);
  log.blank();

  log.info(
    "Mint keypair secret (save this if you need to prove mint ownership):",
  );
  log.blank();
  console.log(
    `  ${Buffer.from(mintKeypair.secretKey).toString("hex").slice(0, 32)}…`,
  );
  log.blank();

  log.info("Next steps:");
  log.kv(
    "Mint tokens",
    `npx tsx src/mint-tokens.ts --to <wallet> --amount <n>`,
  );
  log.kv("Check info", `npx tsx src/token-info.ts`);
  log.kv(
    "Verify ownership",
    `npx tsx src/verify-ownership.ts --wallet <pubkey>`,
  );
  log.kv("Revoke minting", `npx tsx src/revoke-mint.ts   (⚠ irreversible)`);
  log.blank();
}

main().catch((err) => {
  log.error("Failed to create token:", err);
  process.exit(1);
});
