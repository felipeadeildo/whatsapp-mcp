from datetime import datetime

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from fastmcp.server.dependencies import AccessToken, get_access_token

from .auth import WhatsAppAuth
from .config import get_settings


def create_mcp_server() -> FastMCP:
    """Cria e configura o servidor MCP."""
    auth_manager = WhatsAppAuth()
    auth_provider = auth_manager.get_auth_provider()

    mcp = FastMCP(
        name="WhatsApp MCP Server",
        auth=auth_provider,
        instructions="""
        Este servidor fornece funcionalidades do WhatsApp via MCP.
        Você precisa estar autenticado para usar as ferramentas.
        """,
    )

    @mcp.tool
    async def send_message(phone: str, message: str) -> dict:
        """Envia uma mensagem via WhatsApp."""
        access_token: AccessToken | None = get_access_token()
        if access_token is None:
            raise ToolError("Usuário precisa estar autenticado")

        user_id = access_token.client_id

        if "whatsapp:send" not in access_token.scopes:
            raise ToolError("Usuário não tem permissão para enviar mensagens")

        # TODO: Implementar lógica real do WhatsApp aqui
        # whatsapp_session = get_user_whatsapp_session(user_id)
        # result = await whatsapp_session.send_message(phone, message)

        return {
            "user_id": user_id,
            "phone": phone,
            "message": message,
            "status": "sent",
            "timestamp": "2025-01-17T10:00:00Z",
            "note": "Funcionalidade ainda não implementada - apenas simulação",
        }

    @mcp.tool
    async def get_user_info() -> dict:
        """Retorna informações do usuário autenticado."""
        access_token: AccessToken | None = get_access_token()
        if access_token is None:
            raise ToolError("Usuário precisa estar autenticado")

        return {
            "user_id": access_token.client_id,
            "scopes": access_token.scopes,
            "expires_at": datetime.fromtimestamp(access_token.expires_at).isoformat()
            if access_token.expires_at
            else None,
        }

    @mcp.tool
    async def list_chats() -> dict:
        """Lista conversas do WhatsApp do usuário."""
        access_token: AccessToken | None = get_access_token()
        if access_token is None:
            raise ToolError("Usuário precisa estar autenticado")

        user_id = access_token.client_id

        if "whatsapp:read" not in access_token.scopes:
            raise ToolError("Usuário não tem permissão para ler conversas")

        # TODO: Implementar lógica real do WhatsApp aqui
        return {
            "user_id": user_id,
            "chats": [
                {"id": "1", "name": "Família", "last_message": "Oi pessoal!"},
                {"id": "2", "name": "Trabalho", "last_message": "Reunião às 14h"},
            ],
            "note": "Funcionalidade ainda não implementada - apenas simulação",
        }

    return mcp


def run_server():
    """Executa o servidor MCP."""
    config = get_settings()
    server_config = config.server

    mcp = create_mcp_server()

    print("🚀 Iniciando WhatsApp MCP Server...")
    print(f"\tHost: {server_config.host}")
    print(f"\tPort: {server_config.port}")
    print(f"\tTransport: {server_config.transport}")
    print(f"\tURL: http://{server_config.host}:{server_config.port}/mcp/")

    mcp.run(
        transport=server_config.transport,
        host=server_config.host,
        port=server_config.port,
    )
