import os
import secrets
from starlette.authentication import AuthenticationBackend, AuthCredentials, SimpleUser
from starlette.requests import HTTPConnection

API_KEY_FILE = os.path.join(os.path.dirname(os.path.abspath(__file__)), "api_key.txt")


def load_api_key() -> str:
    """Load the API key from the environment or file, creating one if needed."""
    env_key = os.getenv("MCP_API_KEY")
    if env_key:
        return env_key.strip()
    if os.path.exists(API_KEY_FILE):
        with open(API_KEY_FILE, "r") as f:
            key = f.read().strip()
            if key:
                return key
    key = secrets.token_hex(32)
    with open(API_KEY_FILE, "w") as f:
        f.write(key)
    return key


class SimpleAuthBackend(AuthenticationBackend):
    """Simple bearer token authentication using a single API key."""

    def __init__(self, api_key: str):
        self.api_key = api_key

    async def authenticate(self, conn: HTTPConnection):
        auth_header = conn.headers.get("Authorization")
        if auth_header and auth_header.startswith("Bearer "):
            token = auth_header[7:]
            if secrets.compare_digest(token, self.api_key):
                return AuthCredentials(["authenticated"]), SimpleUser("mcp")
        return None
