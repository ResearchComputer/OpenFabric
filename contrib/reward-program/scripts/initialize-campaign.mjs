#!/usr/bin/env node
import * as anchor from '@coral-xyz/anchor';
import {
  ASSOCIATED_TOKEN_PROGRAM_ID,
  TOKEN_2022_PROGRAM_ID,
  TOKEN_PROGRAM_ID,
  getAssociatedTokenAddressSync,
  getOrCreateAssociatedTokenAccount,
  transferChecked,
} from '@solana/spl-token';
import {
  Connection,
  Keypair,
  PublicKey,
  SystemProgram,
  clusterApiUrl,
} from '@solana/web3.js';
import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';
import { fileURLToPath } from 'node:url';

const ROOT_DIR = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const DEPLOYMENTS_DIR = path.join(ROOT_DIR, 'deployments');

function env(name, fallback = '') {
  return process.env[name] || fallback;
}

function requireEnv(name, fallback = '') {
  const value = env(name, fallback);
  if (!value) {
    throw new Error(`Missing required environment variable ${name}`);
  }
  return value;
}

function expandHome(filePath) {
  if (filePath === '~') return process.env.HOME || filePath;
  if (filePath.startsWith('~/')) return path.join(process.env.HOME || '~', filePath.slice(2));
  return filePath;
}

function readKeypair(filePath) {
  const expanded = expandHome(filePath);
  const raw = JSON.parse(fs.readFileSync(expanded, 'utf8'));
  return Keypair.fromSecretKey(Uint8Array.from(raw));
}

function readEnvFile(filePath) {
  if (!fs.existsSync(filePath)) return {};
  const out = {};
  for (const line of fs.readFileSync(filePath, 'utf8').split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith('#') || !trimmed.includes('=')) continue;
    const idx = trimmed.indexOf('=');
    let value = trimmed.slice(idx + 1);
    if (
      (value.startsWith('"') && value.endsWith('"')) ||
      (value.startsWith("'") && value.endsWith("'"))
    ) {
      value = value.slice(1, -1);
    }
    out[trimmed.slice(0, idx)] = value;
  }
  return out;
}

function loadDotEnv(filePath) {
  for (const [key, value] of Object.entries(readEnvFile(filePath))) {
    if (!process.env[key]) process.env[key] = value;
  }
}

function resolveClusterUrl(cluster) {
  if (cluster.startsWith('http://') || cluster.startsWith('https://')) return cluster;
  if (cluster === 'localnet') return 'http://127.0.0.1:8899';
  return clusterApiUrl(cluster);
}

function tokenProgramIdFromEnv() {
  const explicit = env('TOKEN_PROGRAM_ID');
  if (explicit) return new PublicKey(explicit);

  switch (env('TOKEN_PROGRAM', 'token')) {
    case 'token':
      return TOKEN_PROGRAM_ID;
    case 'token-2022':
      return TOKEN_2022_PROGRAM_ID;
    default:
      throw new Error('TOKEN_PROGRAM must be token or token-2022 unless TOKEN_PROGRAM_ID is set');
  }
}

function readIdl(programName) {
  const idlPath = path.join(ROOT_DIR, 'target', 'idl', `${programName}.json`);
  if (!fs.existsSync(idlPath)) {
    throw new Error(`IDL not found at ${idlPath}; run anchor build first`);
  }
  return JSON.parse(fs.readFileSync(idlPath, 'utf8'));
}

function programFromIdl(idl, provider, programId) {
  const idlWithAddress = { ...idl, address: programId.toBase58() };
  return new anchor.Program(idlWithAddress, provider);
}

async function main() {
  loadDotEnv(path.join(ROOT_DIR, '.env'));

  const cluster = env('SOLANA_CLUSTER', 'devnet');
  const deploymentEnv = readEnvFile(path.join(DEPLOYMENTS_DIR, cluster, 'token.env'));
  const programEnv = readEnvFile(path.join(DEPLOYMENTS_DIR, cluster, 'program.env'));

  const walletPath = requireEnv('WALLET', '~/.config/solana/id.json');
  const authority = readKeypair(walletPath);
  const clusterUrl = resolveClusterUrl(cluster);
  const connection = new Connection(clusterUrl, 'confirmed');
  const wallet = new anchor.Wallet(authority);
  const provider = new anchor.AnchorProvider(connection, wallet, {
    commitment: 'confirmed',
    preflightCommitment: 'confirmed',
  });
  anchor.setProvider(provider);

  const programName = env('PROGRAM_NAME', 'opentela_rewards');
  const idl = readIdl(programName);
  const programId = new PublicKey(
    requireEnv('REWARD_PROGRAM_ID', programEnv.REWARD_PROGRAM_ID || idl.address),
  );
  const program = programFromIdl(idl, provider, programId);

  const rewardMint = new PublicKey(requireEnv('TOKEN_MINT', deploymentEnv.TOKEN_MINT));
  const tokenProgram = tokenProgramIdFromEnv();
  const tokenDecimals = Number(env('TOKEN_DECIMALS', deploymentEnv.TOKEN_DECIMALS || '9'));
  const rewardPerUnit = new anchor.BN(requireEnv('REWARD_PER_UNIT', '1'));
  const headAuthority = new PublicKey(requireEnv('HEAD_AUTHORITY', authority.publicKey.toBase58()));
  const fundAmount = env('CAMPAIGN_FUND_AMOUNT_BASE_UNITS', env('CAMPAIGN_FUND_AMOUNT', '0'));

  const [campaign] = PublicKey.findProgramAddressSync(
    [Buffer.from('campaign'), authority.publicKey.toBuffer(), rewardMint.toBuffer()],
    programId,
  );
  const [vaultAuthority] = PublicKey.findProgramAddressSync(
    [Buffer.from('vault_authority'), campaign.toBuffer()],
    programId,
  );
  const vault = getAssociatedTokenAddressSync(
    rewardMint,
    vaultAuthority,
    true,
    tokenProgram,
    ASSOCIATED_TOKEN_PROGRAM_ID,
  );

  console.log('Initializing OpenTela reward campaign');
  console.log(`  cluster:          ${cluster}`);
  console.log(`  rpc:              ${clusterUrl}`);
  console.log(`  authority:        ${authority.publicKey.toBase58()}`);
  console.log(`  head authority:   ${headAuthority.toBase58()}`);
  console.log(`  reward mint:      ${rewardMint.toBase58()}`);
  console.log(`  reward per unit:  ${rewardPerUnit.toString()}`);
  console.log(`  token program:    ${tokenProgram.toBase58()}`);
  console.log(`  campaign:         ${campaign.toBase58()}`);
  console.log(`  vault authority:  ${vaultAuthority.toBase58()}`);
  console.log(`  vault:            ${vault.toBase58()}`);
  console.log();

  const existing = await connection.getAccountInfo(campaign);
  if (existing) {
    console.log('Campaign account already exists; skipping initialize_campaign.');
  } else {
    const signature = await program.methods
      .initializeCampaign(rewardPerUnit, headAuthority)
      .accounts({
        authority: authority.publicKey,
        rewardMint,
        campaign,
        vaultAuthority,
        vault,
        tokenProgram,
        associatedTokenProgram: ASSOCIATED_TOKEN_PROGRAM_ID,
        systemProgram: SystemProgram.programId,
      })
      .rpc();
    console.log(`initialize_campaign signature: ${signature}`);
  }

  if (fundAmount !== '0' && fundAmount !== '0.0') {
    const source = await getOrCreateAssociatedTokenAccount(
      connection,
      authority,
      rewardMint,
      authority.publicKey,
      false,
      'confirmed',
      undefined,
      tokenProgram,
      ASSOCIATED_TOKEN_PROGRAM_ID,
    );
    const rawAmount = BigInt(fundAmount);
    const signature = await transferChecked(
      connection,
      authority,
      source.address,
      rewardMint,
      vault,
      authority,
      rawAmount,
      tokenDecimals,
      [],
      undefined,
      tokenProgram,
    );
    console.log(`fund vault signature: ${signature}`);
  }

  const deploymentDir = path.join(DEPLOYMENTS_DIR, cluster);
  fs.mkdirSync(deploymentDir, { recursive: true });
  fs.writeFileSync(
    path.join(deploymentDir, 'campaign.env'),
    [
      `REWARD_CAMPAIGN=${campaign.toBase58()}`,
      `REWARD_VAULT=${vault.toBase58()}`,
      `REWARD_VAULT_AUTHORITY=${vaultAuthority.toBase58()}`,
      `HEAD_AUTHORITY=${headAuthority.toBase58()}`,
      `REWARD_PER_UNIT=${rewardPerUnit.toString()}`,
      '',
    ].join('\n'),
  );

  console.log();
  console.log(`Campaign launch data written to deployments/${cluster}/campaign.env`);
}

main().catch((error) => {
  console.error(error instanceof Error ? error.message : error);
  process.exit(1);
});
