import os
from typing import Any, Dict, List, Optional

from fastapi import FastAPI, Form, Request
from fastapi.responses import HTMLResponse, RedirectResponse
from fastapi.templating import Jinja2Templates
from fastmcp import FastMCP
from starlette.middleware import Middleware
from starlette.middleware.authentication import AuthenticationMiddleware
from whatsapp import Contact, Message
from whatsapp import download_media as whatsapp_download_media
from whatsapp import get_chat as whatsapp_get_chat
from whatsapp import get_contact_chats as whatsapp_get_contact_chats
from whatsapp import get_direct_chat_by_contact as whatsapp_get_direct_chat_by_contact
from whatsapp import get_last_interaction as whatsapp_get_last_interaction
from whatsapp import get_message_context as whatsapp_get_message_context
from whatsapp import list_chats as whatsapp_list_chats
from whatsapp import list_messages as whatsapp_list_messages
from whatsapp import search_contacts as whatsapp_search_contacts
from whatsapp import send_audio_message as whatsapp_audio_voice_message
from whatsapp import send_file as whatsapp_send_file
from whatsapp import send_message as whatsapp_send_message

from auth import (
    SimpleAuthBackend,
    create_api_key,
    create_session,
    create_user,
    init_db,
    list_api_keys,
    verify_user,
)

# Initialize FastMCP server
mcp = FastMCP("whatsapp")


@mcp.tool()
def search_contacts(query: str) -> List[Contact]:
    """Search WhatsApp contacts by name or phone number.

    Args:
        query: Search term to match against contact names or phone numbers
    """
    contacts = whatsapp_search_contacts(query)
    return contacts


@mcp.tool()
def list_messages(
    after: Optional[str] = None,
    before: Optional[str] = None,
    sender_phone_number: Optional[str] = None,
    chat_jid: Optional[str] = None,
    query: Optional[str] = None,
    limit: int = 20,
    page: int = 0,
    include_context: bool = True,
    context_before: int = 1,
    context_after: int = 1,
) -> List[Message]:
    """Get WhatsApp messages matching specified criteria with optional context.

    Args:
        after: Optional ISO-8601 formatted string to only return messages after this date
        before: Optional ISO-8601 formatted string to only return messages before this date
        sender_phone_number: Optional phone number to filter messages by sender
        chat_jid: Optional chat JID to filter messages by chat
        query: Optional search term to filter messages by content
        limit: Maximum number of messages to return (default 20)
        page: Page number for pagination (default 0)
        include_context: Whether to include messages before and after matches (default True)
        context_before: Number of messages to include before each match (default 1)
        context_after: Number of messages to include after each match (default 1)
    """
    messages = whatsapp_list_messages(
        after=after,
        before=before,
        sender_phone_number=sender_phone_number,
        chat_jid=chat_jid,
        query=query,
        limit=limit,
        page=page,
        include_context=include_context,
        context_before=context_before,
        context_after=context_after,
    )
    return messages


@mcp.tool()
def list_chats(
    query: Optional[str] = None,
    limit: int = 20,
    page: int = 0,
    include_last_message: bool = True,
    sort_by: str = "last_active",
) -> List[Chat]:
    """Get WhatsApp chats matching specified criteria.

    Args:
        query: Optional search term to filter chats by name or JID
        limit: Maximum number of chats to return (default 20)
        page: Page number for pagination (default 0)
        include_last_message: Whether to include the last message in each chat (default True)
        sort_by: Field to sort results by, either "last_active" or "name" (default "last_active")
    """
    chats = whatsapp_list_chats(
        query=query,
        limit=limit,
        page=page,
        include_last_message=include_last_message,
        sort_by=sort_by,
    )
    return chats


@mcp.tool()
def get_chat(chat_jid: str, include_last_message: bool = True) -> Optional[Chat]:
    """Get WhatsApp chat metadata by JID.

    Args:
        chat_jid: The JID of the chat to retrieve
        include_last_message: Whether to include the last message (default True)
    """
    chat = whatsapp_get_chat(chat_jid, include_last_message)
    return chat


@mcp.tool()
def get_direct_chat_by_contact(sender_phone_number: str) -> Optional[Chat]:
    """Get WhatsApp chat metadata by sender phone number.

    Args:
        sender_phone_number: The phone number to search for
    """
    chat = whatsapp_get_direct_chat_by_contact(sender_phone_number)
    return chat


@mcp.tool()
def get_contact_chats(jid: str, limit: int = 20, page: int = 0) -> List[Chat]:
    """Get all WhatsApp chats involving the contact.

    Args:
        jid: The contact's JID to search for
        limit: Maximum number of chats to return (default 20)
        page: Page number for pagination (default 0)
    """
    chats = whatsapp_get_contact_chats(jid, limit, page)
    return chats


@mcp.tool()
def get_last_interaction(jid: str) -> str:
    """Get most recent WhatsApp message involving the contact.

    Args:
        jid: The JID of the contact to search for
    """
    message = whatsapp_get_last_interaction(jid)
    return message


@mcp.tool()
def get_message_context(
    message_id: str, before: int = 5, after: int = 5
) -> MessageContext:
    """Get context around a specific WhatsApp message.

    Args:
        message_id: The ID of the message to get context for
        before: Number of messages to include before the target message (default 5)
        after: Number of messages to include after the target message (default 5)
    """
    context = whatsapp_get_message_context(message_id, before, after)
    return context


@mcp.tool()
def send_message(recipient: str, message: str) -> Dict[str, Any]:
    """Send a WhatsApp message to a person or group. For group chats use the JID.

    Args:
        recipient: The recipient - either a phone number with country code but no + or other symbols,
                 or a JID (e.g., "123456789@s.whatsapp.net" or a group JID like "123456789@g.us")
        message: The message text to send

    Returns:
        A dictionary containing success status and a status message
    """
    # Validate input
    if not recipient:
        return {"success": False, "message": "Recipient must be provided"}

    # Call the whatsapp_send_message function with the unified recipient parameter
    success, status_message = whatsapp_send_message(recipient, message)
    return {"success": success, "message": status_message}


@mcp.tool()
def send_file(recipient: str, media_path: str) -> Dict[str, Any]:
    """Send a file such as a picture, raw audio, video or document via WhatsApp to the specified recipient. For group messages use the JID.

    Args:
        recipient: The recipient - either a phone number with country code but no + or other symbols,
                 or a JID (e.g., "123456789@s.whatsapp.net" or a group JID like "123456789@g.us")
        media_path: The absolute path to the media file to send (image, video, document)

    Returns:
        A dictionary containing success status and a status message
    """

    # Call the whatsapp_send_file function
    success, status_message = whatsapp_send_file(recipient, media_path)
    return {"success": success, "message": status_message}


@mcp.tool()
def send_audio_message(recipient: str, media_path: str) -> Dict[str, Any]:
    """Send any audio file as a WhatsApp audio message to the specified recipient. For group messages use the JID. If it errors due to ffmpeg not being installed, use send_file instead.

    Args:
        recipient: The recipient - either a phone number with country code but no + or other symbols,
                 or a JID (e.g., "123456789@s.whatsapp.net" or a group JID like "123456789@g.us")
        media_path: The absolute path to the audio file to send (will be converted to Opus .ogg if it's not a .ogg file)

    Returns:
        A dictionary containing success status and a status message
    """
    success, status_message = whatsapp_audio_voice_message(recipient, media_path)
    return {"success": success, "message": status_message}


@mcp.tool()
def download_media(message_id: str, chat_jid: str) -> Dict[str, Any]:
    """Download media from a WhatsApp message and get the local file path.

    Args:
        message_id: The ID of the message containing the media
        chat_jid: The JID of the chat containing the message

    Returns:
        A dictionary containing success status, a status message, and the file path if successful
    """
    file_path = whatsapp_download_media(message_id, chat_jid)

    if file_path:
        return {
            "success": True,
            "message": "Media downloaded successfully",
            "file_path": file_path,
        }
    else:
        return {"success": False, "message": "Failed to download media"}


if __name__ == "__main__":
    port = int(os.getenv("MCP_SERVER_PORT", 8000))
    host = os.getenv("MCP_SERVER_HOST", "0.0.0.0")

    init_db()

    templates = Jinja2Templates(directory=os.path.join(os.path.dirname(__file__), "templates"))

    backend = SimpleAuthBackend()
    middleware = [Middleware(AuthenticationMiddleware, backend=backend)]
    app = FastAPI(middleware=middleware)

    mcp_app = mcp.http_app(transport="sse")
    app.mount("/mcp", mcp_app)

    def current_user(request: Request) -> Optional[int]:
        if request.user and request.user.is_authenticated:
            return int(request.user.identity)
        return None

    @app.get("/")
    def root(request: Request):
        user = current_user(request)
        if user:
            return RedirectResponse("/keys")
        return RedirectResponse("/login")

    @app.get("/register", response_class=HTMLResponse)
    def register_form(request: Request):
        return templates.TemplateResponse("register.html", {"request": request})

    @app.post("/register")
    def register(username: str = Form(...), password: str = Form(...)):
        if create_user(username, password):
            return RedirectResponse("/login", status_code=303)
        return HTMLResponse("User already exists", status_code=400)

    @app.get("/login", response_class=HTMLResponse)
    def login_form(request: Request):
        return templates.TemplateResponse("login.html", {"request": request})

    @app.post("/login")
    def login(username: str = Form(...), password: str = Form(...)):
        user_id = verify_user(username, password)
        if user_id:
            token = create_session(user_id)
            response = RedirectResponse("/keys", status_code=303)
            response.set_cookie("session_token", token, httponly=True)
            return response
        return HTMLResponse("Invalid credentials", status_code=400)

    @app.get("/keys", response_class=HTMLResponse)
    def keys(request: Request):
        user = current_user(request)
        if not user:
            return RedirectResponse("/login")
        keys_list = list_api_keys(user)
        return templates.TemplateResponse("keys.html", {"request": request, "keys": keys_list})

    @app.post("/keys/new")
    def new_key(request: Request):
        user = current_user(request)
        if not user:
            return RedirectResponse("/login")
        create_api_key(user)
        return RedirectResponse("/keys", status_code=303)

    @app.get("/logout")
    def logout():
        response = RedirectResponse("/login", status_code=303)
        response.delete_cookie("session_token")
        return response

    import uvicorn

    uvicorn.run(app, host=host, port=port)
