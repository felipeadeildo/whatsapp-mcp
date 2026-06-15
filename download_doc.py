"""Download and decrypt WhatsApp document (PDF) for a given message_id."""
import sqlite3, hashlib, hmac, os, math, requests, sys
from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes
from cryptography.hazmat.backends import default_backend

DB_PATH = r"C:\Projects\whatsapp-mcp\data\db\messages.db"
CDN_BASE = "https://mmg.whatsapp.net"
OUT_DIR = r"C:\Projects\whatsapp-mcp\data\media\documents"

MESSAGE_IDS = [
    "3EB03155626E5F0F101CB8",  # notificacao extrajudicial (older)
    "3EB08259202E2404BEE3FE",  # notificacao extrajudicial (newer copy)
]

def hkdf_expand(key, info, length):
    salt = b'\x00' * 32
    prk = hmac.new(salt, key, hashlib.sha256).digest()
    n = math.ceil(length / 32)
    okm, t = b"", b""
    for i in range(1, n + 1):
        t = hmac.new(prk, t + info + bytes([i]), hashlib.sha256).digest()
        okm += t
    return okm[:length]

def wa_decrypt(media_key, enc_data, media_type=b"WhatsApp Document Keys"):
    derivative = hkdf_expand(media_key, media_type, 112)
    iv, cipher_key = derivative[:16], derivative[16:48]
    ciphertext = enc_data[:-10]
    cipher = Cipher(algorithms.AES(cipher_key), modes.CBC(iv), backend=default_backend())
    decryptor = cipher.decryptor()
    padded = decryptor.update(ciphertext) + decryptor.finalize()
    pad_len = padded[-1]
    return padded[:-pad_len]

os.makedirs(OUT_DIR, exist_ok=True)
con = sqlite3.connect(DB_PATH)
cur = con.cursor()

for mid in MESSAGE_IDS:
    cur.execute("SELECT file_name, media_key, direct_path FROM media_metadata WHERE message_id=?", (mid,))
    row = cur.fetchone()
    if not row:
        print(f"[{mid[:8]}] not found"); continue
    file_name, media_key_blob, direct_path = row
    url = CDN_BASE + direct_path
    safe_name = file_name.encode('ascii', 'replace').decode()
    print(f"[{mid[:8]}] downloading {safe_name}...")
    try:
        r = requests.get(url, timeout=30, headers={"User-Agent": "WhatsApp/2.24.6.77 A"})
        r.raise_for_status()
    except Exception as e:
        print(f"[{mid[:8]}] FAILED: {e}"); continue
    try:
        data = wa_decrypt(bytes(media_key_blob), r.content)
    except Exception as e:
        print(f"[{mid[:8]}] decrypt failed: {e}"); continue
    out = os.path.join(OUT_DIR, f"{mid[:8]}_{file_name}")
    with open(out, "wb") as f:
        f.write(data)
    print(f"[{mid[:8]}] saved to {out} ({len(data)} bytes)")

con.close()
