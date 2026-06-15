#!/usr/bin/env bash
#
# Smoke-test runner for whatsapp-mcp PR.
# Calls each of the 5 new MCP tools against a real WhatsApp account.
#
# Requires: server running on :8088 with MCP_API_KEY=smoketest-key-2026,
# ffmpeg in PATH, fixtures in this directory.
set -euo pipefail

SERVER='http://localhost:8088/mcp/smoketest-key-2026/'
TARGET_JID='5521977206368@s.whatsapp.net'
FIXTURES="$(cd "$(dirname "$0")" && pwd)"

# ANSI colors for the human reader
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
RESET='\033[0m'

# --- session bootstrap ---
echo -e "${BLUE}[init]${RESET} establishing MCP session..."
INIT_RESP=$(curl -s -i -X POST "$SERVER" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"smoketest","version":"1.0"}}}')

SESSION_ID=$(echo "$INIT_RESP" | grep -i '^Mcp-Session-Id:' | tr -d '\r' | awk '{print $2}')
if [ -z "$SESSION_ID" ]; then
  echo -e "${RED}FAIL${RESET}: no session ID returned"
  echo "$INIT_RESP"
  exit 1
fi
echo -e "${GREEN}OK${RESET}: session=$SESSION_ID"

# notify initialized
curl -s -X POST "$SERVER" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -H "Mcp-Session-Id: $SESSION_ID" \
  -d '{"jsonrpc":"2.0","method":"notifications/initialized"}' > /dev/null

# --- helper: call a tool, return the result body ---
call_tool() {
  local id="$1"; shift
  local payload="$1"; shift
  curl -s -X POST "$SERVER" \
    -H 'Content-Type: application/json' \
    -H 'Accept: application/json, text/event-stream' \
    -H "Mcp-Session-Id: $SESSION_ID" \
    -d "$payload"
}

# --- helper: extract message ID from a tool result text "(message_id: ABCD)" or "Message sent successfully to ..." ---
extract_msg_id() {
  echo "$1" | grep -oE 'message_id: [A-Z0-9]+' | head -1 | cut -d' ' -f2
}

run_test() {
  local n="$1"; shift
  local label="$1"; shift
  local payload="$1"; shift
  echo -e "\n${BLUE}[test $n]${RESET} $label"
  local resp=$(call_tool "$n" "$payload")
  echo "  $resp" | head -c 400
  echo ""
  if echo "$resp" | grep -qE '"isError":true|"error"'; then
    echo -e "  ${RED}FAIL${RESET}"
    return 1
  else
    echo -e "  ${GREEN}OK${RESET}"
    return 0
  fi
}

# --- TEST 0: send a baseline text message we can later edit/delete/react to ---
echo -e "\n${YELLOW}=== TEST 0: seed text message (for later edit/react/delete) ===${RESET}"
PAYLOAD=$(jq -nc --arg jid "$TARGET_JID" '{
  jsonrpc:"2.0",id:100,method:"tools/call",
  params:{name:"send_message",arguments:{chat_jid:$jid,text:"smoke test seed message — will be edited"}}
}')
RESP=$(call_tool 100 "$PAYLOAD")
echo "  $RESP" | head -c 300; echo
SEED_TEXT_OK=$?

# --- TEST 1: send image ---
PAYLOAD=$(jq -nc --arg jid "$TARGET_JID" --arg p "$FIXTURES/image.png" '{
  jsonrpc:"2.0",id:1,method:"tools/call",
  params:{name:"send_file",arguments:{chat_jid:$jid,file_path:$p,caption:"test image (640x480 teal)"}}
}')
run_test 1 "send_file PNG" "$PAYLOAD" || true

# --- TEST 2: send video ---
PAYLOAD=$(jq -nc --arg jid "$TARGET_JID" --arg p "$FIXTURES/video.mp4" '{
  jsonrpc:"2.0",id:2,method:"tools/call",
  params:{name:"send_file",arguments:{chat_jid:$jid,file_path:$p,caption:"test video (5s pattern)"}}
}')
run_test 2 "send_file MP4" "$PAYLOAD" || true

# --- TEST 3: send document ---
PAYLOAD=$(jq -nc --arg jid "$TARGET_JID" --arg p "$FIXTURES/smoke.pdf" '{
  jsonrpc:"2.0",id:3,method:"tools/call",
  params:{name:"send_file",arguments:{chat_jid:$jid,file_path:$p,caption:"test PDF"}}
}')
run_test 3 "send_file PDF" "$PAYLOAD" || true

# --- TEST 4: send voice note (mp3 → opus via ffmpeg) ---
PAYLOAD=$(jq -nc --arg jid "$TARGET_JID" --arg p "$FIXTURES/audio.mp3" '{
  jsonrpc:"2.0",id:4,method:"tools/call",
  params:{name:"send_audio_message",arguments:{chat_jid:$jid,audio_path:$p}}
}')
run_test 4 "send_audio_message MP3 (ffmpeg conversion)" "$PAYLOAD" || true

# --- pause for echoes to come back so DB has the IDs ---
sleep 3

# --- pull the most recent message ID we sent (last is_from_me=1 row in target chat) ---
DB_PATH='/c/Projects/whatsapp-mcp/data/db/messages.db'
SQLITE='/c/Users/carla/scoop/apps/android-clt/current/platform-tools/sqlite3'
SEED_ID=$("$SQLITE" "$DB_PATH" "SELECT id FROM messages WHERE chat_jid='$TARGET_JID' AND is_from_me=1 AND text LIKE 'smoke test seed%' ORDER BY timestamp DESC LIMIT 1")
echo -e "\n${YELLOW}=== Last seed text msg id: $SEED_ID ===${RESET}"

# --- TEST 5: react to seed message with ❤️ ---
PAYLOAD=$(jq -nc --arg jid "$TARGET_JID" --arg id "$SEED_ID" '{
  jsonrpc:"2.0",id:5,method:"tools/call",
  params:{name:"send_reaction",arguments:{chat_jid:$jid,message_id:$id,emoji:"❤️"}}
}')
run_test 5 "send_reaction ❤️" "$PAYLOAD" || true

sleep 2

# --- TEST 6: remove reaction (empty emoji) ---
PAYLOAD=$(jq -nc --arg jid "$TARGET_JID" --arg id "$SEED_ID" '{
  jsonrpc:"2.0",id:6,method:"tools/call",
  params:{name:"send_reaction",arguments:{chat_jid:$jid,message_id:$id,emoji:""}}
}')
run_test 6 "send_reaction (remove)" "$PAYLOAD" || true

sleep 2

# --- TEST 7: edit seed message ---
PAYLOAD=$(jq -nc --arg jid "$TARGET_JID" --arg id "$SEED_ID" '{
  jsonrpc:"2.0",id:7,method:"tools/call",
  params:{name:"edit_message",arguments:{chat_jid:$jid,message_id:$id,new_text:"smoke test seed message — EDITED via MCP"}}
}')
run_test 7 "edit_message" "$PAYLOAD" || true

sleep 2

# --- TEST 8: send another text and then delete (revoke) it ---
PAYLOAD=$(jq -nc --arg jid "$TARGET_JID" '{
  jsonrpc:"2.0",id:80,method:"tools/call",
  params:{name:"send_message",arguments:{chat_jid:$jid,text:"this message will be deleted"}}
}')
RESP=$(call_tool 80 "$PAYLOAD")
echo "  seed-for-delete: $RESP" | head -c 200; echo

sleep 2
DELETE_ID=$("$SQLITE" "$DB_PATH" "SELECT id FROM messages WHERE chat_jid='$TARGET_JID' AND is_from_me=1 AND text='this message will be deleted' ORDER BY timestamp DESC LIMIT 1")
echo "  delete target id: $DELETE_ID"

PAYLOAD=$(jq -nc --arg jid "$TARGET_JID" --arg id "$DELETE_ID" '{
  jsonrpc:"2.0",id:8,method:"tools/call",
  params:{name:"delete_message",arguments:{chat_jid:$jid,message_id:$id}}
}')
run_test 8 "delete_message (revoke)" "$PAYLOAD" || true

sleep 3

# --- DB verification ---
echo -e "\n${YELLOW}=== DB consistency checks ===${RESET}"
echo "--- seed msg state ---"
"$SQLITE" "$DB_PATH" "SELECT id, text, edited_at, deleted_at FROM messages WHERE id='$SEED_ID'" 2>&1
echo "--- delete-target state ---"
"$SQLITE" "$DB_PATH" "SELECT id, text, edited_at, deleted_at FROM messages WHERE id='$DELETE_ID'" 2>&1
echo "--- reactions on seed msg ---"
"$SQLITE" "$DB_PATH" "SELECT message_id, sender_jid, emoji, timestamp FROM message_reactions WHERE message_id='$SEED_ID'" 2>&1
echo "--- recent outbound msgs in target chat (last 8) ---"
"$SQLITE" "$DB_PATH" "SELECT id, message_type, text FROM messages WHERE chat_jid='$TARGET_JID' AND is_from_me=1 ORDER BY timestamp DESC LIMIT 8" 2>&1

echo -e "\n${GREEN}=== smoke test runner complete ===${RESET}"
