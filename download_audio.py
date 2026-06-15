"""Download and decrypt WhatsApp audio messages that were skipped by auto-download."""
import sqlite3, hashlib, hmac, struct, os, requests
from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes
from cryptography.hazmat.backends import default_backend

DB_PATH = r"C:\Projects\whatsapp-mcp\data\db\messages.db"
MEDIA_DIR = r"C:\Projects\whatsapp-mcp\data\media\audio"
CDN_BASE = "https://mmg.whatsapp.net"

MESSAGE_IDS = [
    "3EB0AC762B768D0972BF15",
    "3A0C7C0EA9D2905DA16B",
    "AC240E3F1CC2477ED3D1B49261160D28",
    "3ACD37931BC45B30A9DF",
]

def hkdf_expand(key: bytes, info: bytes, length: int) -> bytes:
    """HKDF-Extract + Expand (RFC 5869) with zero salt."""
    import hashlib, hmac, math
    # Extract
    salt = b'\x00' * 32
    prk = hmac.new(salt, key, hashlib.sha256).digest()
    # Expand
    n = math.ceil(length / 32)
    okm = b""
    t = b""
    for i in range(1, n + 1):
        t = hmac.new(prk, t + info + bytes([i]), hashlib.sha256).digest()
        okm += t
    return okm[:length]

def wa_decrypt_audio(media_key: bytes, enc_data: bytes) -> bytes:
    """Decrypt WhatsApp audio using HKDF-derived keys."""
    derivative = hkdf_expand(media_key, b"WhatsApp Audio Keys", 112)
    iv = derivative[:16]
    cipher_key = derivative[16:48]
    # mac_key = derivative[48:80]  # skip MAC verification for now
    # last 10 bytes are MAC
    ciphertext = enc_data[:-10]
    cipher = Cipher(algorithms.AES(cipher_key), modes.CBC(iv), backend=default_backend())
    decryptor = cipher.decryptor()
    padded = decryptor.update(ciphertext) + decryptor.finalize()
    # PKCS7 unpad
    pad_len = padded[-1]
    return padded[:-pad_len]

os.makedirs(MEDIA_DIR, exist_ok=True)
con = sqlite3.connect(DB_PATH)
cur = con.cursor()

for mid in MESSAGE_IDS:
    cur.execute(
        "SELECT file_name, media_key, direct_path FROM media_metadata WHERE message_id=?", (mid,)
    )
    row = cur.fetchone()
    if not row:
        print(f"[{mid[:8]}] not found in DB")
        continue
    file_name, media_key_blob, direct_path = row
    if not media_key_blob or not direct_path:
        print(f"[{mid[:8]}] missing media_key or direct_path")
        continue

    url = CDN_BASE + direct_path
    print(f"[{mid[:8]}] downloading {file_name} from CDN...")
    try:
        r = requests.get(url, timeout=30, headers={"User-Agent": "WhatsApp/2.24.6.77 A"})
        r.raise_for_status()
    except Exception as e:
        print(f"[{mid[:8]}] download failed: {e}")
        continue

    print(f"[{mid[:8]}] decrypting ({len(r.content)} bytes enc)...")
    try:
        audio_data = wa_decrypt_audio(bytes(media_key_blob), r.content)
    except Exception as e:
        print(f"[{mid[:8]}] decrypt failed: {e}")
        continue

    out_path = os.path.join(MEDIA_DIR, file_name)
    with open(out_path, "wb") as f:
        f.write(audio_data)

    rel_path = "audio/" + file_name
    cur.execute(
        "UPDATE media_metadata SET download_status='downloaded', file_path=? WHERE message_id=?",
        (rel_path, mid)
    )
    con.commit()
    print(f"[{mid[:8]}] saved to {out_path} ({len(audio_data)} bytes) OK")

con.close()
print("Done.")
