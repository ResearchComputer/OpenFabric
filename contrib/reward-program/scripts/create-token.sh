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
FORCE_NEW_TOKEN="${FORCE_NEW_TOKEN:-false}"
MINT_INITIAL_SUPPLY_ON_EXISTING="${MINT_INITIAL_SUPPLY_ON_EXISTING:-false}"

TOKEN_PROGRAM="${TOKEN_PROGRAM:-token}"
TOKEN_PROGRAM_ID="${TOKEN_PROGRAM_ID:-}"
TOKEN_DECIMALS="${TOKEN_DECIMALS:-9}"
TOKEN_INITIAL_SUPPLY="${TOKEN_INITIAL_SUPPLY:-0}"
TOKEN_MINT="${TOKEN_MINT:-}"
TOKEN_KEYPAIR="${TOKEN_KEYPAIR:-}"
TOKEN_AUTHORITY_KEYPAIR="${TOKEN_AUTHORITY_KEYPAIR:-$WALLET}"
TOKEN_RECIPIENT="${TOKEN_RECIPIENT:-}"
ENABLE_FREEZE="${ENABLE_FREEZE:-false}"
REVOKE_MINT_AUTHORITY="${REVOKE_MINT_AUTHORITY:-false}"
REVOKE_FREEZE_AUTHORITY="${REVOKE_FREEZE_AUTHORITY:-false}"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

json_get_mint() {
  node -e '
    const fs = require("node:fs");
    const raw = fs.readFileSync(0, "utf8");
    const parsed = JSON.parse(raw);
    const mint =
      parsed.address ||
      parsed.mint ||
      parsed.token ||
      parsed.tokenAddress ||
      parsed.commandOutput?.address ||
      parsed.commandOutput?.mint ||
      parsed.commandOutput?.token ||
      parsed.commandOutput?.tokenAddress;
    if (!mint) {
      console.error("Unable to find mint address in spl-token JSON output:");
      console.error(raw);
      process.exit(1);
    }
    process.stdout.write(mint);
  '
}

require_cmd solana
require_cmd solana-keygen
require_cmd spl-token
require_cmd node

if [[ "$SOLANA_CLUSTER" == "mainnet-beta" && "$CONFIRM_MAINNET" != "yes" ]]; then
  echo "Refusing to create a mainnet token without CONFIRM_MAINNET=yes." >&2
  exit 1
fi

DEPLOYMENT_TOKEN_ENV="deployments/$SOLANA_CLUSTER/token.env"
if [[ -z "$TOKEN_MINT" && "$FORCE_NEW_TOKEN" != "true" && -f "$DEPLOYMENT_TOKEN_ENV" ]]; then
  TOKEN_MINT="$(awk -F= '$1 == "TOKEN_MINT" { print $2; exit }' "$DEPLOYMENT_TOKEN_ENV")"
fi
CREATED_NEW_TOKEN=false

TOKEN_ARGS=()
case "$TOKEN_PROGRAM" in
  token)
    ;;
  token-2022)
    TOKEN_ARGS+=(--program-2022)
    ;;
  *)
    if [[ -n "$TOKEN_PROGRAM_ID" ]]; then
      TOKEN_ARGS+=(-p "$TOKEN_PROGRAM_ID")
    else
      echo "TOKEN_PROGRAM must be token or token-2022 unless TOKEN_PROGRAM_ID is set." >&2
      exit 1
    fi
    ;;
esac

AUTHORITY_ADDRESS="$(solana address -k "$TOKEN_AUTHORITY_KEYPAIR")"
if [[ -z "$TOKEN_RECIPIENT" ]]; then
  TOKEN_RECIPIENT="$AUTHORITY_ADDRESS"
fi

CREATE_ARGS=(
  create-token
  --url "$SOLANA_CLUSTER"
  --fee-payer "$WALLET"
  --mint-authority "$AUTHORITY_ADDRESS"
  --decimals "$TOKEN_DECIMALS"
  --output json-compact
  "${TOKEN_ARGS[@]}"
)

if [[ "$ENABLE_FREEZE" == "true" ]]; then
  CREATE_ARGS+=(--enable-freeze)
fi

if [[ -n "$TOKEN_KEYPAIR" ]]; then
  CREATE_ARGS+=("$TOKEN_KEYPAIR")
fi

if [[ -n "$TOKEN_MINT" ]]; then
  CREATE_JSON="$(printf '{"reused":true,"address":"%s"}' "$TOKEN_MINT")"
  echo "Using existing OTELA mint"
else
  echo "Creating OTELA mint"
fi
echo "  cluster:          $SOLANA_CLUSTER"
echo "  fee payer:        $WALLET"
echo "  mint authority:   $AUTHORITY_ADDRESS"
echo "  decimals:         $TOKEN_DECIMALS"
echo "  token program:    $TOKEN_PROGRAM${TOKEN_PROGRAM_ID:+ ($TOKEN_PROGRAM_ID)}"
if [[ -n "$TOKEN_MINT" ]]; then
  echo "  mint:             $TOKEN_MINT"
fi
echo

if [[ -z "$TOKEN_MINT" ]]; then
  CREATE_JSON="$(spl-token "${CREATE_ARGS[@]}")"
  TOKEN_MINT="$(printf "%s" "$CREATE_JSON" | json_get_mint)"
  CREATED_NEW_TOKEN=true
  echo "Created mint: $TOKEN_MINT"
fi

if [[ "$TOKEN_INITIAL_SUPPLY" != "0" && "$TOKEN_INITIAL_SUPPLY" != "0.0" ]]; then
  if [[ "$CREATED_NEW_TOKEN" != "true" && "$MINT_INITIAL_SUPPLY_ON_EXISTING" != "true" ]]; then
    echo "Skipping initial supply mint for existing mint $TOKEN_MINT."
    echo "Set MINT_INITIAL_SUPPLY_ON_EXISTING=true to mint TOKEN_INITIAL_SUPPLY anyway."
  else
    echo
    echo "Minting initial supply"
    echo "  amount:           $TOKEN_INITIAL_SUPPLY"
    echo "  recipient owner:  $TOKEN_RECIPIENT"

    if ! spl-token accounts "$TOKEN_MINT" \
      --owner "$TOKEN_RECIPIENT" \
      --url "$SOLANA_CLUSTER" \
      "${TOKEN_ARGS[@]}" \
      --addresses-only | grep -q .; then
      spl-token create-account "$TOKEN_MINT" \
        --owner "$TOKEN_RECIPIENT" \
        --url "$SOLANA_CLUSTER" \
        --fee-payer "$WALLET" \
        "${TOKEN_ARGS[@]}"
    fi

    spl-token mint "$TOKEN_MINT" "$TOKEN_INITIAL_SUPPLY" \
      --recipient-owner "$TOKEN_RECIPIENT" \
      --url "$SOLANA_CLUSTER" \
      --fee-payer "$WALLET" \
      --mint-authority "$TOKEN_AUTHORITY_KEYPAIR" \
      "${TOKEN_ARGS[@]}"
  fi
fi

if [[ "$REVOKE_MINT_AUTHORITY" == "true" ]]; then
  echo
  echo "Revoking mint authority. This is irreversible."
  spl-token authorize "$TOKEN_MINT" mint --disable \
    --authority "$TOKEN_AUTHORITY_KEYPAIR" \
    --url "$SOLANA_CLUSTER" \
    --fee-payer "$WALLET" \
    "${TOKEN_ARGS[@]}"
fi

if [[ "$REVOKE_FREEZE_AUTHORITY" == "true" ]]; then
  echo
  echo "Revoking freeze authority. This is irreversible."
  spl-token authorize "$TOKEN_MINT" freeze --disable \
    --authority "$TOKEN_AUTHORITY_KEYPAIR" \
    --url "$SOLANA_CLUSTER" \
    --fee-payer "$WALLET" \
    "${TOKEN_ARGS[@]}"
fi

mkdir -p "deployments/$SOLANA_CLUSTER"
{
  echo "TOKEN_MINT=$TOKEN_MINT"
  echo "TOKEN_DECIMALS=$TOKEN_DECIMALS"
  echo "TOKEN_PROGRAM=$TOKEN_PROGRAM"
  echo "TOKEN_PROGRAM_ID=$TOKEN_PROGRAM_ID"
} >"deployments/$SOLANA_CLUSTER/token.env"
printf "%s\n" "$CREATE_JSON" >"deployments/$SOLANA_CLUSTER/create-token.json"

echo
echo "Token launch data written to deployments/$SOLANA_CLUSTER/"
echo "Set TOKEN_MINT=$TOKEN_MINT in downstream reward configuration."
