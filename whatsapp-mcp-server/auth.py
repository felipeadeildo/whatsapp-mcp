import hashlib
import os
import secrets
import sqlite3
from datetime import datetime, timedelta
from typing import Optional

from starlette.authentication import (
    AuthenticationBackend,
    AuthCredentials,
    SimpleUser,
)
from starlette.requests import HTTPConnection

DB_PATH = os.path.join(os.path.dirname(os.path.abspath(__file__)), "auth.db")


def init_db():
    conn = sqlite3.connect(DB_PATH)
    c = conn.cursor()
    c.execute(
        """
        CREATE TABLE IF NOT EXISTS users (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            username TEXT UNIQUE NOT NULL,
            password_hash TEXT NOT NULL
        )
        """
    )
    c.execute(
        """
        CREATE TABLE IF NOT EXISTS api_keys (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            user_id INTEGER NOT NULL,
            api_key TEXT UNIQUE NOT NULL,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY(user_id) REFERENCES users(id)
        )
        """
    )
    c.execute(
        """
        CREATE TABLE IF NOT EXISTS sessions (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            user_id INTEGER NOT NULL,
            session_token TEXT UNIQUE NOT NULL,
            expires_at TIMESTAMP NOT NULL,
            FOREIGN KEY(user_id) REFERENCES users(id)
        )
        """
    )
    conn.commit()
    conn.close()


def hash_password(password: str) -> str:
    return hashlib.sha256(password.encode()).hexdigest()


def create_user(username: str, password: str) -> bool:
    try:
        conn = sqlite3.connect(DB_PATH)
        c = conn.cursor()
        c.execute(
            "INSERT INTO users (username, password_hash) VALUES (?, ?)",
            (username, hash_password(password)),
        )
        conn.commit()
        return True
    except sqlite3.IntegrityError:
        return False
    finally:
        conn.close()


def verify_user(username: str, password: str) -> Optional[int]:
    conn = sqlite3.connect(DB_PATH)
    c = conn.cursor()
    c.execute(
        "SELECT id, password_hash FROM users WHERE username=?",
        (username,),
    )
    row = c.fetchone()
    conn.close()
    if row and row[1] == hash_password(password):
        return row[0]
    return None


def create_api_key(user_id: int) -> str:
    key = secrets.token_hex(32)
    conn = sqlite3.connect(DB_PATH)
    c = conn.cursor()
    c.execute(
        "INSERT INTO api_keys (user_id, api_key) VALUES (?, ?)",
        (user_id, key),
    )
    conn.commit()
    conn.close()
    return key


def list_api_keys(user_id: int):
    conn = sqlite3.connect(DB_PATH)
    c = conn.cursor()
    c.execute(
        "SELECT api_key, created_at FROM api_keys WHERE user_id=?",
        (user_id,),
    )
    keys = c.fetchall()
    conn.close()
    return keys


def verify_api_key(key: str) -> Optional[int]:
    conn = sqlite3.connect(DB_PATH)
    c = conn.cursor()
    c.execute(
        "SELECT user_id FROM api_keys WHERE api_key=?",
        (key,),
    )
    row = c.fetchone()
    conn.close()
    if row:
        return row[0]
    return None


def create_session(user_id: int, ttl_hours: int = 24) -> str:
    token = secrets.token_hex(32)
    expires_at = datetime.utcnow() + timedelta(hours=ttl_hours)
    conn = sqlite3.connect(DB_PATH)
    c = conn.cursor()
    c.execute(
        "INSERT INTO sessions (user_id, session_token, expires_at) VALUES (?, ?, ?)",
        (user_id, token, expires_at.isoformat()),
    )
    conn.commit()
    conn.close()
    return token


def verify_session(token: str) -> Optional[int]:
    conn = sqlite3.connect(DB_PATH)
    c = conn.cursor()
    c.execute(
        "SELECT user_id, expires_at FROM sessions WHERE session_token=?",
        (token,),
    )
    row = c.fetchone()
    conn.close()
    if row:
        expires = datetime.fromisoformat(row[1])
        if expires > datetime.utcnow():
            return row[0]
    return None


class AuthUser(SimpleUser):
    """User object that exposes the numeric ID via the identity property."""

    def __init__(self, user_id: int):
        super().__init__(str(user_id))
        self.user_id = user_id

    @property
    def identity(self) -> str:  # pragma: no cover - starlette compatibility
        return str(self.user_id)


class SimpleAuthBackend(AuthenticationBackend):
    async def authenticate(self, conn: HTTPConnection):
        token = None
        auth_header = conn.headers.get("Authorization")
        if auth_header and auth_header.lower().startswith("bearer "):
            token = auth_header[7:]
            user_id = verify_api_key(token)
            if user_id:
                return AuthCredentials(["authenticated"]), AuthUser(user_id)

        cookie_token = conn.cookies.get("session_token")
        if cookie_token:
            user_id = verify_session(cookie_token)
            if user_id:
                return AuthCredentials(["authenticated"]), AuthUser(user_id)
        return None
