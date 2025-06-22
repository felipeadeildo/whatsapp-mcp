import hashlib
import secrets
from datetime import datetime
from typing import Any

from fastmcp.server.auth import BearerAuthProvider
from fastmcp.server.auth.providers.bearer import RSAKeyPair

from .config import KEYS_DIR, get_settings, load_users, save_users
from .wuzapi_client import WuzapiClient

PRIVATE_KEY_FILE = KEYS_DIR / "private_key.pem"
PUBLIC_KEY_FILE = KEYS_DIR / "public_key.pem"


class WhatsAppAuth:
    def __init__(self):
        self.config = get_settings()
        self.key_pair = self.load_or_generate_keys()

    def load_or_generate_keys(self) -> RSAKeyPair:
        """Carrega chaves existentes ou gera novas."""
        if PRIVATE_KEY_FILE.exists() and PUBLIC_KEY_FILE.exists():
            print("🔑 Carregando chaves existentes...")
            with open(PRIVATE_KEY_FILE) as f:
                private_key = f.read()
            with open(PUBLIC_KEY_FILE) as f:
                public_key = f.read()

            # Cria RSAKeyPair com chaves existentes
            key_pair = RSAKeyPair(private_key=private_key, public_key=public_key)  # type: ignore
            return key_pair
        else:
            return self.generate_new_keys()

    def generate_new_keys(self) -> RSAKeyPair:
        """Gera e salva novas chaves RSA."""
        key_pair = RSAKeyPair.generate()

        # Salva chaves
        with open(PRIVATE_KEY_FILE, "w") as f:
            f.write(key_pair.private_key.get_secret_value())
        with open(PUBLIC_KEY_FILE, "w") as f:
            f.write(key_pair.public_key)

        print(f"✅ Chaves salvas em: {KEYS_DIR}")
        return key_pair

    def get_auth_provider(self) -> BearerAuthProvider:
        """Retorna o provider de autenticação configurado."""
        return BearerAuthProvider(
            public_key=self.key_pair.public_key,
            issuer=self.config.auth.issuer,
            audience=self.config.auth.audience,
        )

    def create_user(
        self, username: str, password: str, scopes: list[str] | None = None
    ) -> dict[str, Any]:
        """Cria um novo usuário local e no wuzapi."""
        if scopes is None:
            scopes = ["whatsapp:send", "whatsapp:read"]

        users = load_users()

        if username in users:
            raise ValueError(f"Usuário '{username}' já existe")

        # Gera token único para o wuzapi
        wuzapi_token = secrets.token_urlsafe(20)

        # Gera webhook URL único para o usuário
        webhook_url = f"{self.config.server.url}/webhook/{username}"

        # Hash da senha
        password_hash = hashlib.sha256(password.encode()).hexdigest()

        user_data = {
            "username": username,
            "password_hash": password_hash,
            "scopes": scopes,
            "created_at": datetime.now().isoformat(),
            "active": True,
            "wuzapi_token": wuzapi_token,
            "webhook_url": webhook_url,
            "wuzapi_user_id": None,
        }

        # Cria usuário no wuzapi
        try:
            with WuzapiClient() as wuzapi:
                wuzapi_user_id = wuzapi.create_user(
                    name=username, token=wuzapi_token, webhook=webhook_url
                )
                user_data["wuzapi_user_id"] = wuzapi_user_id
                print(f"✅ Usuário criado no wuzapi com ID: {wuzapi_user_id}")
                print(
                    "Conecte-se ao WhatsApp por meio do link:\n"
                    f"\t{self.config.wuzapi.base_url}/dashboard"
                )
        except Exception as e:
            print(f"⚠️ Erro ao criar usuário no wuzapi: {e}")
            print("\tUsuário será criado apenas localmente")

        users[username] = user_data
        save_users(users)

        return user_data

    def create_token(self, username: str) -> str:
        """Cria token para um usuário."""
        users = load_users()

        if username not in users:
            raise ValueError(f"Usuário '{username}' não encontrado")

        user = users[username]
        if not user.get("active", True):
            raise ValueError(f"Usuário '{username}' está inativo")

        return self.key_pair.create_token(
            subject=username,
            issuer=self.config.auth.issuer,
            audience=self.config.auth.audience,
            scopes=user["scopes"],
            expires_in_seconds=self.config.auth.token_expiry_hours * 3600,
        )

    def authenticate_user(self, username: str, password: str) -> bool:
        """Autentica um usuário."""
        users = load_users()

        if username not in users:
            return False

        user = users[username]
        password_hash = hashlib.sha256(password.encode()).hexdigest()

        return user["password_hash"] == password_hash and user.get("active", True)
