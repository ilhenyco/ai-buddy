"""Generate once; give token to client, put only hash in server configuration."""

import hashlib
import secrets

token = secrets.token_urlsafe(32)
print("Client token:", token)
print("Server token_sha256:", hashlib.sha256(token.encode()).hexdigest())
