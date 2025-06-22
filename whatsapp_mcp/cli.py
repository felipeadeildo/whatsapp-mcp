import argparse
import getpass
import sys

from .auth import WhatsAppAuth
from .config import load_users
from .server import run_server


def cmd_generate_keys(args):
    """Gera novas chaves RSA."""
    print("🔑 Gerando novas chaves RSA...")
    auth = WhatsAppAuth()
    auth.generate_new_keys()
    print("✅ Chaves geradas com sucesso!")


def cmd_create_user(args):
    """Cria um novo usuário."""
    username = args.username

    if not username:
        username = input("Username: ")

    password = getpass.getpass("Password: ")
    confirm_password = getpass.getpass("Confirme a password: ")

    if password != confirm_password:
        print("❌ Senhas não coincidem!")
        sys.exit(1)

    # Define scopes
    scopes = args.scopes if args.scopes else ["whatsapp:send", "whatsapp:read"]

    try:
        auth = WhatsAppAuth()
        user_data = auth.create_user(username, password, scopes)

        print(f"✅ Usuário '{username}' criado com sucesso!")
        print(f"\tScopes: {', '.join(user_data['scopes'])}")
        print(f"\tCriado em: {user_data['created_at']}")

        # Gera token para teste
        if args.generate_token:
            token = auth.create_token(username)
            print("\n🎫 Token de acesso:")
            print(f"\t{token}")

    except ValueError as e:
        print(f"❌ Erro: {e}")
        sys.exit(1)


def cmd_generate_token(args):
    """Gera token para um usuário existente."""
    username = args.username

    if not username:
        username = input("Username: ")

    try:
        auth = WhatsAppAuth()
        token = auth.create_token(username)

        print(f"🎫 Token para '{username}':")
        print(f"\t{token}")

    except ValueError as e:
        print(f"❌ Erro: {e}")
        sys.exit(1)


def cmd_list_users(args):
    """Lista usuários cadastrados."""
    users = load_users()

    if not users:
        print("📭 Nenhum usuário cadastrado.")
        return

    print("👥 Usuários cadastrados:")
    for username, user_data in users.items():
        status = "✅ Ativo" if user_data.get("active", True) else "❌ Inativo"
        scopes = ", ".join(user_data.get("scopes", []))
        print(f"\t• {username} - {status} - Scopes: {scopes}")


def cmd_server(args):
    """Executa o servidor MCP."""
    run_server()


def main():
    """Interface CLI principal."""
    parser = argparse.ArgumentParser(
        description="WhatsApp MCP Server - Sistema de autenticação e servidor MCP"
    )

    subparsers = parser.add_subparsers(dest="command", help="Comandos disponíveis")

    # Comando: generate-keys
    keys_parser = subparsers.add_parser("generate-keys", help="Gera novas chaves RSA")
    keys_parser.set_defaults(func=cmd_generate_keys)

    # Comando: create-user
    user_parser = subparsers.add_parser("create-user", help="Cria um novo usuário")
    user_parser.add_argument("username", nargs="?", help="Nome do usuário")
    user_parser.add_argument(
        "--scopes",
        nargs="+",
        help="Scopes do usuário (padrão: whatsapp:send whatsapp:read)",
    )
    user_parser.add_argument(
        "--generate-token", action="store_true", help="Gera token após criar o usuário"
    )
    user_parser.set_defaults(func=cmd_create_user)

    # Comando: generate-token
    token_parser = subparsers.add_parser(
        "generate-token", help="Gera token para usuário"
    )
    token_parser.add_argument("username", nargs="?", help="Nome do usuário")
    token_parser.set_defaults(func=cmd_generate_token)

    # Comando: list-users
    list_parser = subparsers.add_parser("list-users", help="Lista usuários cadastrados")
    list_parser.set_defaults(func=cmd_list_users)

    # Comando: server
    server_parser = subparsers.add_parser("server", help="Executa o servidor MCP")
    server_parser.set_defaults(func=cmd_server)

    args = parser.parse_args()

    if not args.command:
        parser.print_help()
        sys.exit(1)

    args.func(args)


if __name__ == "__main__":
    main()
