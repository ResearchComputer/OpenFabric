"""Tests for the auth server."""

import os
import tempfile

import base58
import pytest
from fastapi.testclient import TestClient
from nacl.signing import SigningKey

# Override DB path before importing server.
_tmpdir = tempfile.mkdtemp()
os.environ.setdefault("AUTH_DB_PATH", os.path.join(_tmpdir, "test.db"))

from auth.models import DB_PATH as _  # noqa: F401, E402
import auth.models as models  # noqa: E402

models.DB_PATH = os.path.join(_tmpdir, "test.db")

from auth.server import app  # noqa: E402

client = TestClient(app)


def _make_wallet():
    """Generate a fresh Ed25519 keypair and return (SigningKey, base58 pubkey)."""
    sk = SigningKey.generate()
    pubkey = base58.b58encode(bytes(sk.verify_key)).decode()
    return sk, pubkey


def _sign_challenge(sk: SigningKey, challenge: str) -> str:
    signed = sk.sign(challenge.encode())
    return base58.b58encode(signed.signature).decode()


class TestCreateKey:
    def test_success(self):
        sk, wallet = _make_wallet()
        challenge = f"otela-auth:{wallet}:12345"
        sig = _sign_challenge(sk, challenge)

        resp = client.post("/api/keys", json={
            "wallet": wallet,
            "signature": sig,
            "challenge": challenge,
            "label": "test-key",
        })
        assert resp.status_code == 200
        data = resp.json()
        assert data["wallet"] == wallet
        assert data["token"].startswith("otela_")
        assert data["key_id"].startswith("okey_")
        assert data["label"] == "test-key"

    def test_bad_challenge_prefix(self):
        sk, wallet = _make_wallet()
        challenge = "wrong-prefix:hello"
        sig = _sign_challenge(sk, challenge)

        resp = client.post("/api/keys", json={
            "wallet": wallet,
            "signature": sig,
            "challenge": challenge,
        })
        assert resp.status_code == 400

    def test_bad_signature(self):
        _, wallet = _make_wallet()
        other_sk = SigningKey.generate()
        challenge = f"otela-auth:{wallet}:12345"
        sig = _sign_challenge(other_sk, challenge)  # wrong key

        resp = client.post("/api/keys", json={
            "wallet": wallet,
            "signature": sig,
            "challenge": challenge,
        })
        assert resp.status_code == 403


class TestVerifyAndRevoke:
    def _create_key(self):
        sk, wallet = _make_wallet()
        challenge = f"otela-auth:{wallet}:99999"
        sig = _sign_challenge(sk, challenge)
        resp = client.post("/api/keys", json={
            "wallet": wallet,
            "signature": sig,
            "challenge": challenge,
        })
        assert resp.status_code == 200
        return resp.json(), wallet

    def test_verify(self):
        data, wallet = self._create_key()
        resp = client.post("/api/keys/verify", json={"token": data["token"]})
        assert resp.status_code == 200
        assert resp.json()["wallet"] == wallet

    def test_verify_invalid_token(self):
        resp = client.post("/api/keys/verify", json={"token": "bogus"})
        assert resp.status_code == 401

    def test_list_keys(self):
        data, wallet = self._create_key()
        resp = client.get("/api/keys", params={"wallet": wallet})
        assert resp.status_code == 200
        keys = resp.json()
        assert any(k["key_id"] == data["key_id"] for k in keys)

    def test_revoke_and_verify_fails(self):
        data, wallet = self._create_key()
        # Revoke
        resp = client.delete(f"/api/keys/{data['key_id']}", params={"wallet": wallet})
        assert resp.status_code == 200

        # Verify should now fail
        resp = client.post("/api/keys/verify", json={"token": data["token"]})
        assert resp.status_code == 401

    def test_revoke_wrong_wallet(self):
        data, _ = self._create_key()
        resp = client.delete(f"/api/keys/{data['key_id']}", params={"wallet": "wrong"})
        assert resp.status_code == 403
