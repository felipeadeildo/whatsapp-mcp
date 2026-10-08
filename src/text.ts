export type Lang = "en" | "pt"

export const LOCALE: Record<Lang, string> = { en: "en", pt: "pt-BR" }

export function requestLang(request: Request): Lang {
  return (request.headers.get("accept-language") ?? "").toLowerCase().startsWith("pt") ? "pt" : "en"
}

const en = {
  credit: "Brought to you by",
  auth: {
    signIn: "Sign in",
    signInLead: "Type the password of this server to manage the WhatsApp account {account}.",
    password: "Password",
    submit: "Sign in",
    wrong: "That password was not accepted. Use the SECRET you set when deploying this server.",
    tooMany: "Too many attempts. Wait a minute and try again.",
    allowTitle: "Let {client} use your WhatsApp?",
    allowLead: "It will be able to read, search and send messages as the account {account}.",
    publishedBy: "Published by {domain}.",
    selfRegistered: "This app registered itself, so its name is not verified.",
    sendsTo: "Access is sent to {host}.",
    loopback:
      "This sends access to an app on your computer. Continue only if you just started connecting from it.",
    allow: "Allow",
    deny: "Deny",
    failed: "This request could not be completed",
  },
  pairing: {
    loading: ["Checking the connection", "One moment."],
    offline: ["The server is not answering", "Trying again in a few seconds."],
    notLinked: [
      "Not linked to WhatsApp yet",
      "Link this account so your agents can read and send its messages. Have your phone at hand.",
    ],
    unlinked: [
      "Your phone unlinked this device",
      "Its archive was cleared. Link it again to continue.",
    ],
    connecting: ["Connecting to WhatsApp", "This takes a few seconds."],
    pairing: [
      "Scan the code with your phone",
      "The code changes every few seconds, so keep this page open until your phone confirms.",
    ],
    steps: [
      "Open WhatsApp on your phone.",
      "Go to Settings, then Linked devices.",
      "Tap Link a device and point the camera at the code.",
    ],
    phoneNote: "On a phone? Open this page on a computer and scan the code with your phone.",
    linked: "Linked as {name}",
    archived:
      "Your agents can read <strong>{messages}</strong> messages from <strong>{chats}</strong> chats, back to {date}.",
    syncStart:
      "Waiting for your phone to send the history. Keep WhatsApp open on it; this can take a few minutes.",
    syncing:
      "Your phone is sending the history. <strong>{messages}</strong> messages from <strong>{chats}</strong> chats have arrived so far. Keep WhatsApp open on it until it finishes.",
    history: "history",
    live: "Receiving messages",
    lastMessage: "last one {ago}",
    fresh: "+{count} new",
    reconnecting: [
      "Reconnecting to WhatsApp",
      "The connection dropped. It comes back on its own within a minute.",
    ],
    lastError: "Last error: ",
    link: "Link WhatsApp",
    linking: "Linking…",
    unlink: "Unlink this device",
    unlinking: "Unlinking…",
    confirmUnlink:
      "Unlink WhatsApp from this server? Its archive is deleted, and linking again means scanning a new code.",
    copy: "Copy",
    copied: "Copied",
    connect: {
      oauth: [
        "Connect your agent",
        "Add the endpoint as a connector in Claude, ChatGPT or any MCP client. It opens this server in your browser, where you type the password and allow it.",
      ],
      api_key: [
        "Connect your agent",
        "Any MCP client works: point it at the endpoint and send the password as a Bearer token in the Authorization header. It is the SECRET you set when deploying.",
      ],
      both: [
        "Connect your agent",
        "Add the endpoint as a connector in Claude, ChatGPT or any MCP client and allow it in your browser. Clients that send a fixed header can send the password as a Bearer token instead. It is the SECRET you set when deploying.",
      ],
    },
    endpoint: "MCP endpoint",
    claude: "Claude Code",
  },
}

const pt: typeof en = {
  credit: "Feito por",
  auth: {
    signIn: "Entrar",
    signInLead: "Digite a senha deste servidor para gerenciar a conta de WhatsApp {account}.",
    password: "Senha",
    submit: "Entrar",
    wrong: "Senha incorreta. Use o SECRET que você definiu no deploy deste servidor.",
    tooMany: "Tentativas demais. Espere um minuto e tente de novo.",
    allowTitle: "Permitir que {client} use seu WhatsApp?",
    allowLead: "Ele poderá ler, buscar e enviar mensagens pela conta {account}.",
    publishedBy: "Publicado por {domain}.",
    selfRegistered: "Este app se registrou sozinho, então o nome dele não é verificado.",
    sendsTo: "O acesso vai para {host}.",
    loopback:
      "Isto envia o acesso para um app no seu computador. Continue só se você acabou de começar a conexão por ele.",
    allow: "Permitir",
    deny: "Negar",
    failed: "Não foi possível concluir este pedido",
  },
  pairing: {
    loading: ["Verificando a conexão", "Um instante."],
    offline: ["O servidor não está respondendo", "Tentando de novo em alguns segundos."],
    notLinked: [
      "Ainda não conectado ao WhatsApp",
      "Conecte esta conta para que seus agentes leiam e enviem mensagens por ela. Tenha o celular em mãos.",
    ],
    unlinked: [
      "Seu celular desconectou este aparelho",
      "O arquivo dele foi apagado. Conecte de novo para continuar.",
    ],
    connecting: ["Conectando ao WhatsApp", "Isso leva alguns segundos."],
    pairing: [
      "Escaneie o código com o celular",
      "O código muda a cada poucos segundos, então deixe esta página aberta até o celular confirmar.",
    ],
    steps: [
      "Abra o WhatsApp no celular.",
      "Vá em Configurações e depois em Aparelhos conectados.",
      "Toque em Conectar um aparelho e aponte a câmera para o código.",
    ],
    phoneNote:
      "Está no celular? Abra esta página num computador e escaneie o código com o celular.",
    linked: "Conectado como {name}",
    archived:
      "Seus agentes podem ler <strong>{messages}</strong> mensagens de <strong>{chats}</strong> conversas, desde {date}.",
    syncStart:
      "Esperando o celular enviar o histórico. Deixe o WhatsApp aberto nele; pode levar alguns minutos.",
    syncing:
      "Seu celular está enviando o histórico. Já chegaram <strong>{messages}</strong> mensagens de <strong>{chats}</strong> conversas. Deixe o WhatsApp aberto nele até terminar.",
    history: "histórico",
    live: "Recebendo mensagens",
    lastMessage: "última {ago}",
    fresh: "+{count} novas",
    reconnecting: [
      "Reconectando ao WhatsApp",
      "A conexão caiu. Ela volta sozinha em até um minuto.",
    ],
    lastError: "Último erro: ",
    link: "Conectar WhatsApp",
    linking: "Conectando…",
    unlink: "Desconectar este aparelho",
    unlinking: "Desconectando…",
    confirmUnlink:
      "Desconectar o WhatsApp deste servidor? O arquivo é apagado, e conectar de novo exige escanear um novo código.",
    copy: "Copiar",
    copied: "Copiado",
    connect: {
      oauth: [
        "Conecte seu agente",
        "Adicione o endpoint como conector no Claude, no ChatGPT ou em qualquer cliente MCP. Ele abre este servidor no navegador, onde você digita a senha e permite o acesso.",
      ],
      api_key: [
        "Conecte seu agente",
        "Qualquer cliente MCP serve: aponte para o endpoint e envie a senha como token Bearer no cabeçalho Authorization. Ela é o SECRET que você definiu no deploy.",
      ],
      both: [
        "Conecte seu agente",
        "Adicione o endpoint como conector no Claude, no ChatGPT ou em qualquer cliente MCP e permita o acesso no navegador. Clientes que enviam um cabeçalho fixo podem enviar a senha como token Bearer. Ela é o SECRET que você definiu no deploy.",
      ],
    },
    endpoint: "Endpoint MCP",
    claude: "Claude Code",
  },
}

export const TEXT: Record<Lang, typeof en> = { en, pt }
