import json
import os
from pathlib import Path
from typing import Any, Literal

from pydantic import BaseModel, Field, model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict

BASE_DIR = Path(__file__).parent.parent
DATA_DIR = BASE_DIR / "data"
KEYS_DIR = DATA_DIR / "keys"
USERS_FILE = DATA_DIR / "users.json"
DEBUG_DIR = BASE_DIR / "debug"

DATA_DIR.mkdir(exist_ok=True)
KEYS_DIR.mkdir(exist_ok=True)
DEBUG_DIR.mkdir(exist_ok=True)


IS_DOCKER = os.getenv("IS_DOCKER", "false").lower() == "true"


class ServerConfig(BaseModel):
    host: str = "127.0.0.1"
    port: int = 8000
    transport: Literal["sse", "streamable-http"] = "streamable-http"

    @property
    def url(self) -> str:
        return f"http://{self.host}:{self.port}" if not IS_DOCKER else f"http://server:{self.port}"


class AuthConfig(BaseModel):
    issuer: str = ""
    audience: str = "whatsapp-users"
    token_expiry_hours: int = 24


class WuzapiConfig(BaseModel):
    base_url: str = "http://localhost:8080"
    admin_token: str = ""
    events: list[str] = ["Message", "ReadReceipt", "HistorySync", "ChatPresence"]

    @model_validator(mode="after")
    def override_base_url_for_docker(self) -> "WuzapiConfig":
        if IS_DOCKER:
            self.base_url = "http://wuzapi:8080"
        return self


class Settings(BaseSettings):
    server: ServerConfig = Field(default_factory=ServerConfig)
    auth: AuthConfig = Field(default_factory=AuthConfig)
    wuzapi: WuzapiConfig = Field(default_factory=WuzapiConfig)

    model_config = SettingsConfigDict(
        env_file=BASE_DIR / ".env",
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


def load_users() -> dict[str, dict[str, Any]]:
    """Carrega usuários do arquivo."""
    if USERS_FILE.exists():
        with open(USERS_FILE) as f:
            return json.load(f)
    return {}


def save_users(users: dict[str, dict[str, Any]]) -> None:
    """Salva usuários no arquivo."""
    with open(USERS_FILE, "w") as f:
        json.dump(users, f, indent=2)
