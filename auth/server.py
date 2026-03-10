"""
FastAPI auth server for OpenTela API key management.

Endpoints:
    POST /api/keys          - Create a new API key (requires wallet signature)
    GET  /api/keys          - List keys for a wallet
    DELETE /api/keys/{id}   - Revoke a key
    POST /api/keys/verify   - Verify a bearer token → returns wallet pubkey

Run:
    uvicorn auth.server:app --host 0.0.0.0 --port 8090
"""

from datetime import datetime, timezone

import base58
from fastapi import FastAPI, HTTPException
from nacl.signing import VerifyKey
from nacl.exceptions import BadSignatureError
from pydantic import BaseModel

from .models import (
    APIKey,
    get_session,
    generate_key_id,
    generate_token,
    hash_token,
)

app = FastAPI(title="OpenTela Auth", version="0.1.0")


# ---------------------------------------------------------------------------
# Request / Response schemas
# ---------------------------------------------------------------------------


class CreateKeyRequest(BaseModel):
    """Create an API key. The client must prove wallet ownership by signing
    a challenge string with their Ed25519 private key."""

    wallet: str  # base58-encoded Ed25519 public key
    signature: str  # base58-encoded signature over the challenge
    challenge: str  # the challenge string that was signed
    label: str = ""  # optional human-readable label


class CreateKeyResponse(BaseModel):
    key_id: str
    token: str  # returned only once — client must save it
    wallet: str
    label: str
    created_at: datetime


class KeyInfo(BaseModel):
    key_id: str
    wallet: str
    label: str
    created_at: datetime
    revoked: bool


class VerifyRequest(BaseModel):
    token: str


class VerifyResponse(BaseModel):
    wallet: str
    key_id: str


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


# Accepted challenge prefixes. The challenge must start with one of these
# so that an attacker cannot trick a user into signing an arbitrary message
# by reusing a signature from another context.
CHALLENGE_PREFIX = "otela-auth:"


def verify_wallet_signature(wallet: str, signature: str, message: str) -> bool:
    """Verify an Ed25519 signature from a Solana/OpenTela wallet."""
    try:
        pub_bytes = base58.b58decode(wallet)
        sig_bytes = base58.b58decode(signature)
        vk = VerifyKey(pub_bytes)
        vk.verify(message.encode(), sig_bytes)
        return True
    except (BadSignatureError, Exception):
        return False


# ---------------------------------------------------------------------------
# Endpoints
# ---------------------------------------------------------------------------


@app.post("/api/keys", response_model=CreateKeyResponse)
def create_key(req: CreateKeyRequest):
    """Create a new API key. Requires a signed challenge to prove wallet
    ownership."""
    if not req.challenge.startswith(CHALLENGE_PREFIX):
        raise HTTPException(
            status_code=400,
            detail=f'Challenge must start with "{CHALLENGE_PREFIX}"',
        )

    if not verify_wallet_signature(req.wallet, req.signature, req.challenge):
        raise HTTPException(status_code=403, detail="Invalid wallet signature")

    token = generate_token()
    key_id = generate_key_id()
    now = datetime.now(timezone.utc)

    session = get_session()
    try:
        row = APIKey(
            key_id=key_id,
            token_hash=hash_token(token),
            wallet=req.wallet,
            label=req.label,
            created_at=now,
        )
        session.add(row)
        session.commit()
    finally:
        session.close()

    return CreateKeyResponse(
        key_id=key_id,
        token=token,
        wallet=req.wallet,
        label=req.label,
        created_at=now,
    )


@app.get("/api/keys", response_model=list[KeyInfo])
def list_keys(wallet: str):
    """List all API keys for a wallet."""
    session = get_session()
    try:
        rows = session.query(APIKey).filter_by(wallet=wallet).all()
        return [
            KeyInfo(
                key_id=r.key_id,
                wallet=r.wallet,
                label=r.label,
                created_at=r.created_at,
                revoked=r.revoked,
            )
            for r in rows
        ]
    finally:
        session.close()


@app.delete("/api/keys/{key_id}")
def revoke_key(key_id: str, wallet: str):
    """Revoke an API key. The wallet query param must match the key's owner."""
    session = get_session()
    try:
        row = session.query(APIKey).filter_by(key_id=key_id).first()
        if not row:
            raise HTTPException(status_code=404, detail="Key not found")
        if row.wallet != wallet:
            raise HTTPException(status_code=403, detail="Wallet mismatch")
        row.revoked = True
        session.commit()
        return {"status": "revoked", "key_id": key_id}
    finally:
        session.close()


@app.post("/api/keys/verify", response_model=VerifyResponse)
def verify_token(req: VerifyRequest):
    """Verify a bearer token and return the associated wallet. This endpoint
    is called by head nodes to resolve a client's identity."""
    session = get_session()
    try:
        h = hash_token(req.token)
        row = session.query(APIKey).filter_by(token_hash=h).first()
        if not row:
            raise HTTPException(status_code=401, detail="Invalid token")
        if row.revoked:
            raise HTTPException(status_code=401, detail="Token has been revoked")
        return VerifyResponse(wallet=row.wallet, key_id=row.key_id)
    finally:
        session.close()
