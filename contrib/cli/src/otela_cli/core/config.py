import os
import json
from pathlib import Path
from typing import Dict, Any

CONFIG_DIR = Path.home() / ".config" / "opentela"
CONFIG_FILE = CONFIG_DIR / "cli_config.json"

DEFAULT_CONFIG = {
    "rpc_url": "http://140.238.223.116:8092",
    "solana_rpc_url": "https://api.devnet.solana.com", # Default to devnet for Solana RPC
    "wallet_path": str(Path.home() / ".config" / "solana" / "id.json"),
    "session_pubkey": None,
    "opentela_token_mint": "xTRCFBHAfjepfKNStvWQ7xmHwFS7aJ85oufa1BoXedL", # Token program ID/Mint from Anchor
    "default_cluster": "default"
}

def load_config() -> Dict[str, Any]:
    if not CONFIG_FILE.exists():
        return DEFAULT_CONFIG.copy()
    try:
        with open(CONFIG_FILE, "r") as f:
            data = json.load(f)
            merged = DEFAULT_CONFIG.copy()
            merged.update(data)
            return merged
    except Exception:
        return DEFAULT_CONFIG.copy()

def save_config(config: Dict[str, Any]) -> None:
    CONFIG_DIR.mkdir(parents=True, exist_ok=True)
    with open(CONFIG_FILE, "w") as f:
        json.dump(config, f, indent=4)
