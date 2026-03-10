"""Tests for the auth server."""

import os
import tempfile
import time

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


def _make_challenge(wallet: str) -> str:
    """Build a fresh, properly-formatted challenge for *wallet*."""
    return f"otela-auth:{wallet}:{int(time.time())}"


def _sign_challenge(sk: SigningKey, challenge: str) -> str:
    signed = sk.sign(challenge.encode())
    return base58.b58encode(signed.signature).decode()


class TestCreateKey:
    def test_success(self):
        sk, wallet = _make_wallet()
        challenge = _make_challenge(wallet)
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
        challenge = f"wrong-prefix:{wallet}:{int(time.time())}"
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
        challenge = _make_challenge(wallet)
        sig = _sign_challenge(other_sk, challenge)  # wrong key

        resp = client.post("/api/keys", json={
            "wallet": wallet,
            "signature": sig,
            "challenge": challenge,
        })
        assert resp.status_code == 403

    def test_challenge_wallet_mismatch(self):
        """Challenge signed for wallet A must not be usable for wallet B."""
        sk_a, wallet_a = _make_wallet()
        _, wallet_b = _make_wallet()
        # Challenge is bound to wallet_a but request claims wallet_b
        challenge = _make_challenge(wallet_a)
        sig = _sign_challenge(sk_a, challenge)

        resp = client.post("/api/keys", json={
            "wallet": wallet_b,
            "signature": sig,
            "challenge": challenge,
        })
        assert resp.status_code == 400

    def test_stale_challenge_rejected(self):
        """A challenge whose timestamp is outside the TTL window is rejected."""
        sk, wallet = _make_wallet()
        old_ts = int(time.time()) - 600  # 10 minutes ago
        challenge = f"otela-auth:{wallet}:{old_ts}"
        sig = _sign_challenge(sk, challenge)

        resp = client.post("/api/keys", json={
            "wallet": wallet,
            "signature": sig,
            "challenge": challenge,
        })
        assert resp.status_code == 400

    def test_future_challenge_rejected(self):
        """A challenge with a timestamp more than 60 s in the future is rejected."""
        sk, wallet = _make_wallet()
        future_ts = int(time.time()) + 600  # 10 minutes from now
        challenge = f"otela-auth:{wallet}:{future_ts}"
        sig = _sign_challenge(sk, challenge)

        resp = client.post("/api/keys", json={
            "wallet": wallet,
            "signature": sig,
            "challenge": challenge,
        })
        assert resp.status_code == 400

    def test_replay_rejected(self):
        """The same signed challenge must not be accepted a second time."""
        sk, wallet = _make_wallet()
        challenge = _make_challenge(wallet)
        sig = _sign_challenge(sk, challenge)

        payload = {"wallet": wallet, "signature": sig, "challenge": challenge}

        resp1 = client.post("/api/keys", json=payload)
        assert resp1.status_code == 200

        resp2 = client.post("/api/keys", json=payload)
        assert resp2.status_code == 400
        assert "already been used" in resp2.json()["detail"]

    def test_bad_challenge_format_missing_parts(self):
        """Challenges that lack the required three colon-separated parts are rejected."""
        sk, wallet = _make_wallet()
        challenge = f"otela-auth:{wallet}"  # missing timestamp
        sig = _sign_challenge(sk, challenge)

        resp = client.post("/api/keys", json={
            "wallet": wallet,
            "signature": sig,
            "challenge": challenge,
        })
        assert resp.status_code == 400

    def test_bad_challenge_non_integer_timestamp(self):
        """A challenge whose timestamp field is not an integer is rejected."""
        sk, wallet = _make_wallet()
        challenge = f"otela-auth:{wallet}:not-a-number"
        sig = _sign_challenge(sk, challenge)

        resp = client.post("/api/keys", json={
            "wallet": wallet,
            "signature": sig,
            "challenge": challenge,
        })
        assert resp.status_code == 400


class TestVerifyAndRevoke:
    def _create_key(self):
        sk, wallet = _make_wallet()
        challenge = _make_challenge(wallet)
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
