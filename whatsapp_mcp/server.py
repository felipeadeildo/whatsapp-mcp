from datetime import datetime

from fastapi import FastAPI, HTTPException, Request
from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from fastmcp.server.dependencies import AccessToken, get_access_token

from .auth import WhatsAppAuth
from .config import get_settings
from .utils import process_webhook_data, save_webhook_event, validate_user_exists


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


def create_fastapi_app() -> FastAPI:
    """Cria aplicação FastAPI com MCP integrado."""
    # Cria o servidor MCP
    mcp = create_mcp_server()

    # Cria o app ASGI do MCP
    mcp_app = mcp.http_app(path="/")

    # Cria a aplicação FastAPI principal
    app = FastAPI(
        title="WhatsApp MCP Server",
        description="Servidor MCP para integração com WhatsApp via wuzapi",
        lifespan=mcp_app.lifespan,
    )

    # Monta o servidor MCP
    app.mount("/mcp", mcp_app)

    # Adiciona webhook listener dinâmico no FastAPI
    @app.get("/webhook/{username}")
    @app.post("/webhook/{username}")
    async def webhook_listener(username: str, request: Request):
        """Recebe webhooks do wuzapi para usuários específicos."""
        try:
            # Valida usuário
            validate_user_exists(username)

            # Processa dados do webhook
            if request.method == "POST":
                data = await process_webhook_data(request)
            else:
                data = dict(request.query_params)

            # Salva evento no debug
            filename = save_webhook_event(
                username=username,
                request_method=request.method,
                headers=dict(request.headers),
                data=data,
            )

            print(f"📨 Webhook recebido para {username}: {filename}")

            return {"status": "ok", "message": "Webhook received"}

        except HTTPException:
            raise
        except Exception as e:
            print(f"❌ Erro no webhook para {username}: {e}")
            raise HTTPException(status_code=500, detail=str(e)) from e

    return app


def run_server() -> None:
    """Executa o servidor FastAPI com MCP integrado."""
    import uvicorn

    config = get_settings()
    server_config = config.server

    print("🚀 Iniciando WhatsApp MCP Server...")
    print(f"\tHost: {server_config.host}")
    print(f"\tPort: {server_config.port}")
    print(f"\tMCP URL: {server_config.url}/mcp")
    print(f"\tWebhooks: {server_config.url}/webhook/{{username}}")

    app = create_fastapi_app()

    uvicorn.run(
        app,
        host=server_config.host,
        port=server_config.port,
    )
