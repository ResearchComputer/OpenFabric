"""Database models and Pydantic schemas for the auth service."""

import secrets
from datetime import datetime, timezone

from sqlalchemy import Column, String, DateTime, Boolean, create_engine
from sqlalchemy.orm import DeclarativeBase, Session, sessionmaker

DB_PATH = "auth.db"


class Base(DeclarativeBase):
    pass


class UsedChallenge(Base):
    """Tracks consumed challenge nonces to prevent replay attacks.

    Each row is retained until ``expires_at`` so that a signed challenge
    cannot be replayed within its validity window.
    """

    __tablename__ = "used_challenges"

    challenge = Column(String, primary_key=True)
    expires_at = Column(DateTime, nullable=False, index=True)


class APIKey(Base):
    __tablename__ = "api_keys"

    key_id = Column(String, primary_key=True)
    token_hash = Column(String, nullable=False, unique=True, index=True)
    wallet = Column(String, nullable=False, index=True)
    label = Column(String, nullable=False, default="")
    created_at = Column(DateTime, nullable=False, default=lambda: datetime.now(timezone.utc))
    revoked = Column(Boolean, nullable=False, default=False)


def get_engine(db_path: str = DB_PATH):
    engine = create_engine(f"sqlite:///{db_path}", echo=False)
    Base.metadata.create_all(engine)
    return engine


def get_session(db_path: str = DB_PATH) -> Session:
    engine = get_engine(db_path)
    return sessionmaker(bind=engine)()


def generate_key_id() -> str:
    return f"okey_{secrets.token_hex(8)}"


def generate_token() -> str:
    return f"otela_{secrets.token_urlsafe(32)}"


def hash_token(token: str) -> str:
    """One-way hash so we never store raw tokens."""
    import hashlib
    return hashlib.sha256(token.encode()).hexdigest()
