from datetime import datetime
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict


class ExternalClient(BaseModel):
    model_config = ConfigDict(extra="forbid")
    # Store only SHA-256 of a randomly generated high-entropy credential.
    token_sha256: str = Field(pattern=r"^[a-f0-9]{64}$")
    scopes: list[Literal["knowledge:search", "knowledge:read", "documents:write"]]
    team_ids: list[str] = []
    expires_at: datetime

    @model_validator(mode="after")
    def timezone_required(self):
        if self.expires_at.tzinfo is None:
            raise ValueError("expires_at must include timezone")
        return self


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_prefix="RAG_", extra="ignore")

    database_url: str = "postgresql://homych:homych@localhost:55432/homych"
    go_url: str = "http://localhost:8080"
    service_token: str = Field(min_length=24)
    # Demo only: token -> Go user ID. Production clients use external_clients.
    mcp_tokens: dict[str, str] = {}
    external_clients: dict[str, ExternalClient] = {}
    allowed_hosts: list[str] = ["localhost:*", "127.0.0.1:*", "[::1]:*"]
    allowed_origins: list[str] = ["http://localhost:*", "http://127.0.0.1:*"]
    demo_mode: bool = False
    synthetic_subjects: list[str] = []
    ollama_url: str = "http://localhost:11434"
    embedding_model: str = "bge-m3:567m"
    generation_model: str = "qwen3:4b"
    provider: str = "ollama"
    min_similarity: float = Field(default=0.5, ge=-1, le=1)
    request_timeout: float = Field(default=60, gt=0)
    embedding_timeout: float = Field(default=15, gt=0)
    generation_timeout: float = Field(default=45, gt=0)
    prompt_bytes: int = Field(default=6000, ge=4000, le=6000)

    @model_validator(mode="after")
    def validate_demo(self):
        import hashlib

        digests = [client.token_sha256 for client in self.external_clients.values()]
        reserved = [self.service_token, *self.mcp_tokens]
        if len(set(digests)) != len(digests) or any(
            hashlib.sha256(token.encode()).hexdigest() in digests for token in reserved
        ):
            raise ValueError(
                "client credentials must be unique and separate from internal/demo tokens"
            )
        if any(not key or key.startswith("service:") for key in self.external_clients):
            raise ValueError("invalid external client ID")
        if self.provider not in {"ollama", "deterministic"}:
            raise ValueError("provider must be ollama or deterministic")
        if not self.demo_mode and (
            self.mcp_tokens or self.synthetic_subjects or self.provider == "deterministic"
        ):
            raise ValueError(
                "demo credentials, fixtures and deterministic provider require demo_mode"
            )
        if any(len(token) < 24 for token in self.mcp_tokens):
            raise ValueError("MCP tokens must have at least 24 characters")
        if self.service_token in self.mcp_tokens:
            raise ValueError("MCP and internal service credentials must be different")
        return self
