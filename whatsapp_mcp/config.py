import json
from pathlib import Path
from typing import Any, Dict, Literal

from pydantic import BaseModel, Field
from pydantic_settings import BaseSettings, SettingsConfigDict

BASE_DIR = Path(__file__).parent.parent
DATA_DIR = BASE_DIR / "data"
KEYS_DIR = DATA_DIR / "keys"
USERS_FILE = DATA_DIR / "users.json"

DATA_DIR.mkdir(exist_ok=True)
KEYS_DIR.mkdir(exist_ok=True)


class ServerConfig(BaseModel):
    host: str = "127.0.0.1"
    port: int = 8000
    transport: Literal["sse", "streamable-http"] = "streamable-http"


class AuthConfig(BaseModel):
    issuer: str = "whatsapp-mcp-server"
    audience: str = "whatsapp-users"
    token_expiry_hours: int = 24


class Settings(BaseSettings):
    server: ServerConfig = Field(default_factory=ServerConfig)
    auth: AuthConfig = Field(default_factory=AuthConfig)

    model_config = SettingsConfigDict(
        env_file=DATA_DIR / "config.env",
        env_nested_delimiter="__",
        json_file=DATA_DIR / "config.json",
        case_sensitive=False,
    )


def get_settings() -> Settings:
    """Retorna as configurações carregadas."""
    return Settings()


def save_config(settings: Settings) -> None:
    """Salva configurações no arquivo JSON."""
    config_file = DATA_DIR / "config.json"
    with open(config_file, "w") as f:
        json.dump(settings.model_dump(), f, indent=2)


def load_users() -> Dict[str, Dict[str, Any]]:
    """Carrega usuários do arquivo."""
    if USERS_FILE.exists():
        with open(USERS_FILE, "r") as f:
            return json.load(f)
    return {}


def save_users(users: Dict[str, Dict[str, Any]]) -> None:
    """Salva usuários no arquivo."""
    with open(USERS_FILE, "w") as f:
        json.dump(users, f, indent=2)
