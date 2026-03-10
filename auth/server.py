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

import time
from datetime import datetime, timedelta, timezone

import base58
from fastapi import FastAPI, HTTPException
from nacl.signing import VerifyKey
from nacl.exceptions import BadSignatureError
from pydantic import BaseModel

from .models import (
    APIKey,
    UsedChallenge,
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


# Challenge format: "otela-auth:<wallet>:<unix_ts>"
# The wallet segment binds the challenge to the requesting key-pair so that a
# signature produced for one wallet cannot be reused by a different one.
CHALLENGE_PREFIX = "otela-auth:"

# Maximum age (in seconds) of a freshly issued challenge.  Challenges older
# than this window are rejected to limit the exposure from a stolen signature.
CHALLENGE_TTL_SECONDS = 300  # 5 minutes


def _validate_challenge(challenge: str, wallet: str) -> None:
    """Validate challenge format, wallet binding, and freshness.

    Expected format: ``otela-auth:<wallet>:<unix_ts>``

    Raises :class:`fastapi.HTTPException` (400) on any validation failure.
    """
    parts = challenge.split(":")
    if len(parts) != 3 or parts[0] != "otela-auth":
        raise HTTPException(
            status_code=400,
            detail='Challenge must be in the format "otela-auth:<wallet>:<unix_ts>"',
        )

    _, ch_wallet, ts_str = parts

    if ch_wallet != wallet:
        raise HTTPException(
            status_code=400,
            detail="Challenge wallet does not match the request wallet",
        )

    try:
        ts = int(ts_str)
    except ValueError:
        raise HTTPException(
            status_code=400,
            detail="Challenge timestamp must be a valid integer",
        )

    now_ts = int(time.time())
    # Reject timestamps too far in the future (allow ≤60 s of clock skew) or
    # too old (older than CHALLENGE_TTL_SECONDS).
    if ts > now_ts + 60:
        raise HTTPException(
            status_code=400,
            detail="Challenge timestamp is too far in the future",
        )
    if now_ts - ts > CHALLENGE_TTL_SECONDS:
        raise HTTPException(
            status_code=400,
            detail="Challenge has expired",
        )


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
    ownership.

    The challenge must follow the format ``otela-auth:<wallet>:<unix_ts>``,
    be signed with the wallet's Ed25519 private key, be no older than
    ``CHALLENGE_TTL_SECONDS``, and must not have been used before (replay
    prevention).
    """
    _validate_challenge(req.challenge, req.wallet)

    if not verify_wallet_signature(req.wallet, req.signature, req.challenge):
        raise HTTPException(status_code=403, detail="Invalid wallet signature")

    token = generate_token()
    key_id = generate_key_id()
    now = datetime.now(timezone.utc)

    session = get_session()
    try:
        # Replay prevention: reject challenges that have already been consumed.
        existing = session.query(UsedChallenge).filter_by(challenge=req.challenge).first()
        if existing:
            raise HTTPException(status_code=400, detail="Challenge has already been used")

        # Purge expired challenge nonces to keep the table bounded.
        # Done probabilistically (1-in-10 chance) to limit overhead on busy
        # paths while still preventing unbounded growth.
        if int(time.time()) % 10 == 0:
            session.query(UsedChallenge).filter(UsedChallenge.expires_at < now).delete()

        # Record this challenge so it cannot be replayed.
        session.add(UsedChallenge(
            challenge=req.challenge,
            expires_at=now + timedelta(seconds=CHALLENGE_TTL_SECONDS),
        ))

        session.add(APIKey(
            key_id=key_id,
            token_hash=hash_token(token),
            wallet=req.wallet,
            label=req.label,
            created_at=now,
        ))
        session.commit()
    except HTTPException:
        session.rollback()
        raise
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
