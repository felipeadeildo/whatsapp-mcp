import json
from datetime import datetime
from typing import Any

from fastapi import HTTPException, Request

from .config import DEBUG_DIR, load_users


async def process_webhook_data(request: Request) -> dict[str, Any]:
    """Processa dados do webhook baseado no content-type."""
    content_type = request.headers.get("content-type", "")

    if "application/json" in content_type:
        return await request.json()
    elif "application/x-www-form-urlencoded" in content_type:
        form_data = await request.form()
        json_data = form_data.get("jsonData")
        if json_data:
            return json.loads(json_data)  # type: ignore
        else:
            return dict(form_data)
    else:
        return await request.json()


def validate_user_exists(username: str) -> None:
    """Valida se o usuário existe."""
    users = load_users()
    if username not in users:
        raise HTTPException(status_code=404, detail="Usuário não encontrado")


def save_webhook_event(
    username: str, request_method: str, headers: dict[str, str], data: ...
) -> str:
    """Salva evento do webhook no diretório debug."""
    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S_%f")
    filename = f"{username}_{timestamp}.json"
    debug_file = DEBUG_DIR / filename

    event_data = {
        "username": username,
        "timestamp": datetime.now().isoformat(),
        "method": request_method,
        "headers": headers,
        "data": data,
    }

    with open(debug_file, "w", encoding="utf-8") as f:
        json.dump(event_data, f, indent=2, ensure_ascii=False)

    return filename


def get_user_from_webhook_data(data: dict[str, Any]) -> str:
    """Identifica usuário baseado nos dados do webhook."""
    # Implementar lógica para identificar usuário dos dados
    # Por enquanto retorna string vazia, será implementado depois
    return ""
