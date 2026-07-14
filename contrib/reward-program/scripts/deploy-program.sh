#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

load_env_preserving_overrides() {
  local env_file="$1"
  local line key value
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line#"${line%%[![:space:]]*}"}"
    line="${line%"${line##*[![:space:]]}"}"
    [[ -z "$line" || "$line" == \#* || "$line" != *=* ]] && continue

    key="${line%%=*}"
    value="${line#*=}"
    key="${key#"${key%%[![:space:]]*}"}"
    key="${key%"${key##*[![:space:]]}"}"
    [[ "$key" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || continue
    [[ -n "${!key+x}" ]] && continue

    value="${value#"${value%%[![:space:]]*}"}"
    value="${value%"${value##*[![:space:]]}"}"
    if [[ "$value" == \"*\" && "$value" == *\" ]]; then
      value="${value:1:${#value}-2}"
    elif [[ "$value" == \'*\' && "$value" == *\' ]]; then
      value="${value:1:${#value}-2}"
    fi
    if [[ "$value" == "~" ]]; then
      value="$HOME"
    elif [[ "$value" == "~/"* ]]; then
      value="$HOME/${value:2}"
    fi
    export "$key=$value"
  done <"$env_file"
}

if [[ -f .env ]]; then
  load_env_preserving_overrides .env
fi

SOLANA_CLUSTER="${SOLANA_CLUSTER:-devnet}"
WALLET="${WALLET:-$HOME/.config/solana/id.json}"
CONFIRM_MAINNET="${CONFIRM_MAINNET:-no}"
PROGRAM_NAME="${PROGRAM_NAME:-opentela_rewards}"
PROGRAM_KEYPAIR="${PROGRAM_KEYPAIR:-target/deploy/opentela_rewards-keypair.json}"
SOLANA_DEPLOY_ARGS="${SOLANA_DEPLOY_ARGS:-}"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

require_cmd anchor
require_cmd solana
require_cmd solana-keygen
require_cmd awk
require_cmd wc

if [[ "$SOLANA_CLUSTER" == "mainnet-beta" && "$CONFIRM_MAINNET" != "yes" ]]; then
  echo "Refusing to deploy to mainnet without CONFIRM_MAINNET=yes." >&2
  exit 1
fi

echo "Building $PROGRAM_NAME"
anchor build

if [[ ! -f "$PROGRAM_KEYPAIR" ]]; then
  echo "Program keypair not found: $PROGRAM_KEYPAIR" >&2
  echo "Generate one with: solana-keygen new --no-bip39-passphrase -o $PROGRAM_KEYPAIR" >&2
  echo "Then sync declare_id!/Anchor.toml with: anchor keys sync" >&2
  exit 1
fi

DECLARED_PROGRAM_ID="$(
  awk -F '"' '/declare_id!/ { print $2; exit }' "programs/$PROGRAM_NAME/src/lib.rs"
)"
KEYPAIR_PROGRAM_ID="$(solana-keygen pubkey "$PROGRAM_KEYPAIR")"

if [[ "$DECLARED_PROGRAM_ID" != "$KEYPAIR_PROGRAM_ID" ]]; then
  cat >&2 <<EOF
Program ID mismatch.

  declare_id!:       $DECLARED_PROGRAM_ID
  program keypair:   $KEYPAIR_PROGRAM_ID
  keypair path:      $PROGRAM_KEYPAIR

Deploying with this keypair would publish a different program address than the
one compiled into the Anchor program.

For a new deployment, either:
  1. Set PROGRAM_KEYPAIR to the keypair whose pubkey is $DECLARED_PROGRAM_ID, or
  2. Intentionally adopt the generated keypair by running:
       anchor keys sync
     then review and commit the changed program ID in Anchor.toml, lib.rs, and docs.
EOF
  exit 1
fi

PROGRAM_SO="target/deploy/$PROGRAM_NAME.so"
if [[ ! -f "$PROGRAM_SO" ]]; then
  echo "Program binary not found after build: $PROGRAM_SO" >&2
  exit 1
fi

PROGRAM_SIZE="$(wc -c <"$PROGRAM_SO" | tr -d ' ')"

echo
echo "Deploying $PROGRAM_NAME"
echo "  cluster:        $SOLANA_CLUSTER"
echo "  wallet:         $WALLET"
echo "  program id:     $DECLARED_PROGRAM_ID"
echo "  binary:         $PROGRAM_SO"
echo "  binary bytes:   $PROGRAM_SIZE"
echo
solana balance --url "$SOLANA_CLUSTER" -k "$WALLET"

DEPLOY_CMD=(
  anchor deploy
  -p "$PROGRAM_NAME"
  --program-keypair "$PROGRAM_KEYPAIR"
  --provider.cluster "$SOLANA_CLUSTER"
  --provider.wallet "$WALLET"
)

if [[ -n "$SOLANA_DEPLOY_ARGS" ]]; then
  # Intentionally split user-supplied deploy args such as "--use-rpc".
  # shellcheck disable=SC2206
  EXTRA_ARGS=($SOLANA_DEPLOY_ARGS)
  "${DEPLOY_CMD[@]}" -- "${EXTRA_ARGS[@]}"
else
  "${DEPLOY_CMD[@]}"
fi

echo
echo "Deployed program:"
solana program show "$DECLARED_PROGRAM_ID" --url "$SOLANA_CLUSTER"

mkdir -p "deployments/$SOLANA_CLUSTER"
{
  echo "REWARD_PROGRAM_ID=$DECLARED_PROGRAM_ID"
  echo "REWARD_PROGRAM_KEYPAIR=$PROGRAM_KEYPAIR"
  echo "REWARD_PROGRAM_SO=$PROGRAM_SO"
} >"deployments/$SOLANA_CLUSTER/program.env"

echo
echo "Program deployment data written to deployments/$SOLANA_CLUSTER/program.env"
