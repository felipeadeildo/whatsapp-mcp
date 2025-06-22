from typing import Any

import httpx
from pydantic import BaseModel

from .config import get_settings


class WuzapiUser(BaseModel):
    id: int
    name: str
    token: str
    webhook: str
    jid: str | None = None
    qrcode: str | None = None
    connected: bool = False
    expiration: int = 0
    events: str


class WuzapiClient:
    def __init__(self):
        settings = get_settings()
        self.config = settings.wuzapi

        if not self.config.admin_token:
            raise ValueError("WUZAPI admin token not configured. Set wuzapi.admin_token in config.")

        self.client = httpx.Client(base_url=self.config.base_url, timeout=30.0)

    def _make_admin_request(self, method: str, endpoint: str, **kwargs) -> dict[str, Any]:
        """Faz requisição com token de admin."""
        headers = kwargs.get("headers", {})
        headers["Authorization"] = self.config.admin_token
        headers["Content-Type"] = "application/json"
        kwargs["headers"] = headers

        response = self.client.request(method, endpoint, **kwargs)
        response.raise_for_status()
        return response.json()

    def _make_user_request(
        self, method: str, endpoint: str, user_token: str, **kwargs
    ) -> dict[str, Any]:
        """Faz requisição com token de usuário."""
        headers = kwargs.get("headers", {})
        headers["Token"] = user_token
        headers["Content-Type"] = "application/json"
        kwargs["headers"] = headers

        response = self.client.request(method, endpoint, **kwargs)
        response.raise_for_status()
        return response.json()

    def list_users(self) -> list[WuzapiUser]:
        """Lista todos os usuários."""
        response = self._make_admin_request("GET", "/admin/users")
        return [WuzapiUser.model_validate(user) for user in response["data"]]

    def create_user(self, name: str, token: str, webhook: str) -> int:
        """Cria um novo usuário com todos os eventos."""
        events_str = ",".join(self.config.events)
        data = {"name": name, "token": token, "webhook": webhook, "events": events_str}
        response = self._make_admin_request("POST", "/admin/users", json=data)
        return response["data"]["id"]

    def delete_user(self, user_id: int) -> dict[str, Any]:
        """Remove um usuário."""
        return self._make_admin_request("DELETE", f"/admin/users/{user_id}")

    def set_webhook(self, user_token: str, webhook_url: str) -> dict[str, Any]:
        """Configura webhook para um usuário."""
        data = {"webhookURL": webhook_url}
        return self._make_user_request("POST", "/webhook", user_token, json=data)

    def get_webhook(self, user_token: str) -> dict[str, Any]:
        """Obtém configuração de webhook de um usuário."""
        return self._make_user_request("GET", "/webhook", user_token)

    def connect_session(self, user_token: str, immediate: bool = False) -> dict[str, Any]:
        """Conecta sessão do WhatsApp com todos os eventos."""
        data = {"Subscribe": self.config.events, "Immediate": immediate}
        return self._make_user_request("POST", "/session/connect", user_token, json=data)

    def disconnect_session(self, user_token: str) -> dict[str, Any]:
        """Desconecta sessão do WhatsApp."""
        return self._make_user_request("POST", "/session/disconnect", user_token)

    def logout_session(self, user_token: str) -> dict[str, Any]:
        """Faz logout da sessão do WhatsApp."""
        return self._make_user_request("POST", "/session/logout", user_token)

    def get_session_status(self, user_token: str) -> dict[str, Any]:
        """Obtém status da sessão."""
        return self._make_user_request("GET", "/session/status", user_token)

    def get_qr_code(self, user_token: str) -> dict[str, Any]:
        """Obtém QR code para autenticação."""
        return self._make_user_request("GET", "/session/qr", user_token)

    def send_text_message(
        self,
        user_token: str,
        phone: str,
        message: str,
        message_id: str | None = None,
    ) -> dict[str, Any]:
        """Envia mensagem de texto."""
        data = {"Phone": phone, "Body": message}
        if message_id:
            data["Id"] = message_id

        return self._make_user_request("POST", "/chat/send/text", user_token, json=data)

    def send_image_message(
        self,
        user_token: str,
        phone: str,
        image_data: str,
        caption: str | None = None,
    ) -> dict[str, Any]:
        """Envia mensagem com imagem."""
        data = {"Phone": phone, "Image": image_data}
        if caption:
            data["Caption"] = caption

        return self._make_user_request("POST", "/chat/send/image", user_token, json=data)

    def get_user_info(self, user_token: str, phones: list[str]) -> dict[str, Any]:
        """Obtém informações de usuários do WhatsApp."""
        data = {"Phone": phones}
        return self._make_user_request("POST", "/user/info", user_token, json=data)

    def check_users(self, user_token: str, phones: list[str]) -> dict[str, Any]:
        """Verifica se números estão no WhatsApp."""
        data = {"Phone": phones}
        return self._make_user_request("POST", "/user/check", user_token, json=data)

    def get_contacts(self, user_token: str) -> dict[str, Any]:
        """Obtém todos os contatos."""
        return self._make_user_request("GET", "/user/contacts", user_token)

    def list_groups(self, user_token: str) -> dict[str, Any]:
        """Lista grupos do usuário."""
        return self._make_user_request("GET", "/group/list", user_token)

    def close(self) -> None:
        """Fecha o cliente HTTP."""
        self.client.close()

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.close()
