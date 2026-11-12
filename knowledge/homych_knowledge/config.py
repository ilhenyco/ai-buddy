from pydantic import Field, model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_prefix="RAG_", extra="ignore")

    database_url: str = "postgresql://homych:homych@localhost:55432/homych"
    go_url: str = "http://localhost:8080"
    service_token: str = Field(min_length=24)
    # Demo only: token -> Go user ID. Empty mapping disables external MCP access.
    mcp_tokens: dict[str, str] = {}
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
