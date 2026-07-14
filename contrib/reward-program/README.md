# OpenTela Reward Program

Anchor scaffold for a fully on-chain OpenTela reward protocol.

The program pays OTELA rewards from a PDA-controlled vault after a provider
submits a usage receipt signed by the campaign's authorized head node. The
receipt signature is verified on-chain through Solana's native Ed25519
verification program:

```text
Ed25519 receipt verification instruction
        |
opentela_rewards::claim_reward
        |
PDA vault transfers OTELA to provider ATA
        |
Claim PDA prevents duplicate request_id payouts
```

## Accounts

- `RewardCampaign` stores the campaign authority, authorized head authority,
  reward mint, PDA vault, reward rate, pause state, and aggregate payout totals.
- `vault` is the associated token account for the reward mint owned by the
  `vault_authority` PDA.
- `ClaimReceipt` is a PDA derived from `(campaign, request_id)` and makes each
  request payout single-use.

## Receipt Message

The campaign's current `head_authority` signs exactly:

```text
"OPENTELA_REWARD_V1"
campaign pubkey          32 bytes
request_id               32 bytes
provider wallet pubkey   32 bytes
service_hash             32 bytes
units                    u64 little-endian
reward_per_unit          u64 little-endian
```

The provider submits that signature as an Ed25519 pre-instruction immediately
before `claim_reward`. If the native Ed25519 instruction fails, the transaction
aborts before the reward program runs. If it succeeds, the reward program checks
that the verified public key equals the campaign's on-chain `head_authority` and
that the verified message matches the claim.

## Instructions

- `initialize_campaign(reward_per_unit, head_authority)` creates the campaign
  PDA and PDA-owned token vault.
- `set_reward_rate(reward_per_unit)` lets the campaign authority update the
  payout rate.
- `set_head_authority(head_authority)` rotates the head node signing authority.
- `set_paused(paused)` pauses or resumes claims.
- `claim_reward(request_id, service_hash, units, reward_per_unit)` verifies the
  immediately preceding Ed25519 receipt instruction, creates the claim PDA, and
  transfers rewards from the vault to the provider ATA.
- `withdraw_unclaimed(amount)` lets the campaign authority recover unclaimed
  vault funds.

## Build

```bash
cd contrib/reward-program
cargo test
anchor build
```

The checked-in `Cargo.lock` pins a few transitive crates to versions compatible
with the Solana SBF Rust 1.84 toolchain bundled with Anchor 0.31.1.

## Scripted Token Mint

Yes, token creation is scriptable. The included script uses the Solana CLI and
`spl-token` CLI to create a classic SPL token mint or a Token Extensions mint,
optionally mint an initial supply, and optionally revoke mint/freeze authorities.

```bash
cd contrib/reward-program
cp .env.example .env
npm install

# Edit .env first. At minimum choose:
#   SOLANA_CLUSTER
#   WALLET
#   TOKEN_DECIMALS
#   TOKEN_INITIAL_SUPPLY
#   TOKEN_PROGRAM
npm run create-token
```

The script writes launch output to:

```text
contrib/reward-program/deployments/<cluster>/token.env
contrib/reward-program/deployments/<cluster>/create-token.json
```

If the mint account was created but the script stopped before writing
`token.env`, set `TOKEN_MINT=<mint-address>` in `.env` or pass it inline and
rerun `npm run create-token`. The script will reuse that mint and continue with
authority changes and deployment output. If the failed run stopped before
minting the initial supply, also set `MINT_INITIAL_SUPPLY_ON_EXISTING=true`.

If `deployments/<cluster>/token.env` already exists, `npm run create-token`
reuses that mint by default and skips `TOKEN_INITIAL_SUPPLY` to avoid duplicate
minting. Set `FORCE_NEW_TOKEN=true` only when intentionally creating another
mint.

For a public OTELA token, treat authority choices as launch policy:

- Keep the mint authority only if the supply is intentionally elastic.
- Revoke the mint authority only after the supply is final; this is
  irreversible.
- Prefer no freeze authority for a neutral public token, or use governance for a
  controlled beta launch.
- Add wallet-facing metadata before public distribution. The lower-level script
  creates the mint and supply; `contrib/ownership/src/create-token.ts` already
  supports Metaplex metadata if you want the richer token-launch path.

## Scripted Program Deploy

Program deployment is also scriptable:

```bash
cd contrib/reward-program
cp .env.example .env
npm install

# Edit .env:
#   SOLANA_CLUSTER=devnet
#   WALLET=~/.config/solana/id.json
#   PROGRAM_KEYPAIR=target/deploy/opentela_rewards-keypair.json
npm run deploy:program
```

The deploy script runs `anchor build`, checks that `PROGRAM_KEYPAIR` matches the
program ID compiled into `declare_id!`, deploys with `anchor deploy`, then writes:

```text
contrib/reward-program/deployments/<cluster>/program.env
```

You do need a wallet for deployment. `WALLET` is the fee payer and, by default,
the upgrade authority. That wallet must hold enough SOL for program deployment
rent and transaction fees. On mainnet, use a dedicated deployment wallet or a
governance-controlled authority rather than a browser hot wallet.

An OpenTela-managed wallet works because it is stored in Solana CLI keypair
format:

```bash
otela wallet create
otela wallet info

# Use the "Keypair file" path printed by wallet info.
WALLET=$HOME/.config/opentela/accounts/<wallet-pubkey>/keypair.json
TOKEN_AUTHORITY_KEYPAIR=$WALLET
```

For devnet, fund it before minting or deploying:

```bash
otela wallet airdrop --solana.rpc https://api.devnet.solana.com 2
solana balance --url devnet -k "$WALLET"
```

The program keypair is intentionally not committed. Before the first deployment
to a real cluster, decide which program ID you want:

```bash
# Generate a new program keypair for this deployment.
solana-keygen new --no-bip39-passphrase \
  -o target/deploy/opentela_rewards-keypair.json

# Sync Anchor.toml and declare_id! to that keypair.
anchor keys sync

# Review and commit the changed program ID in:
#   Anchor.toml
#   programs/opentela_rewards/src/lib.rs
#   docs/content/docs/proposals/2026-07-07-onchain-reward-protocol.mdx
```

If you already generated and published a program ID, set `PROGRAM_KEYPAIR` to
that existing keypair. The deploy script refuses to deploy when the keypair and
`declare_id!` do not match, because that would publish a different address than
the client and IDL expect.

After deployment, copy public addresses into the docs app environment:

```bash
# docs/.env.local or hosting provider env
NEXT_PUBLIC_REWARD_PROGRAM_ID=<deployed-program-id>
NEXT_PUBLIC_REWARD_CAMPAIGN=<initialized-campaign-pda>
NEXT_PUBLIC_OTELA_MINT=<token-mint>
NEXT_PUBLIC_TOKEN_PROGRAM_ID=<token-program-id>
```

These values are public routing/configuration data, not secrets. The docs app
cannot infer them from a connected wallet; it must be configured for the
cluster, program, campaign, mint, and token program it should use.

For mainnet, set `CONFIRM_MAINNET=yes` only after the program has been audited,
the upgrade authority policy is decided, and the deploy wallet has enough SOL.
After a final launch, move the upgrade authority to governance or make the
program immutable with `solana program set-upgrade-authority --final`.

## Scripted Campaign Initialization

Deployment only publishes the program. To start paying rewards, initialize a
campaign and fund its PDA-owned vault:

```bash
cd contrib/reward-program

# The script reads .env plus deployments/<cluster>/{token,program}.env.
# Set these in .env if you want non-default values:
#   REWARD_PER_UNIT=1
#   HEAD_AUTHORITY=<wallet that signs reward receipts>
#   CAMPAIGN_FUND_AMOUNT_BASE_UNITS=1000000000000
npm run init:campaign
```

The script derives the campaign PDA from `(authority, reward_mint)`, creates the
campaign account and associated token vault if they do not already exist, then
writes:

```text
contrib/reward-program/deployments/<cluster>/campaign.env
```

`CAMPAIGN_FUND_AMOUNT_BASE_UNITS` is raw token base units. With 9 decimals,
`1000000000000` funds `1000` OTELA. You can also fund the vault later by sending
OTELA to the `REWARD_VAULT` address written in `campaign.env`.

After the campaign exists, configure head nodes to sign the exact receipt bytes
documented below, and configure providers or a client app to submit an Ed25519
verify instruction immediately before `claim_reward`.

## MVP Security Model

- The campaign authority controls reward rate, pause state, and unclaimed vault
  withdrawals.
- The campaign stores the only head authority whose Ed25519 receipts can unlock
  rewards.
- The provider wallet signs the claim transaction.
- The authorized head node signs the usage receipt.
- One PDA claim account is created per `(campaign, request_id)`, preventing
  duplicate payouts for the same request.

This is intentionally a v1 protocol. Production hardening should add an
on-chain head-node allowlist or stake registry, epoch-based rates, and a dispute
window before high-value mainnet use.

## References

- Solana token mint accounts: https://solana.com/docs/tokens/basics/create-mint
- Solana token authorities: https://solana.com/docs/tokens/basics/set-authority
- Solana program deployment: https://solana.com/docs/programs/deploying
- Anchor local development and deploy: https://www.anchor-lang.com/docs/quickstart/local
