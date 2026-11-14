from datetime import datetime
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator


class StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class AccessContext(StrictModel):
    subjectId: str
    teamIds: list[str]
    internshipIds: list[str]
    role: Literal["intern", "service"]
    synthetic: bool = False


class SearchInput(StrictModel):
    subjectId: str = Field(min_length=1, max_length=80)
    query: str = Field(min_length=1, max_length=1000)
    limit: int = Field(default=5, ge=1, le=5)

    @field_validator("query")
    @classmethod
    def not_blank(cls, value: str) -> str:
        if not value.strip():
            raise ValueError("query must not be blank")
        return value.strip()


class AnswerInput(SearchInput):
    requestId: str = Field(min_length=1, max_length=80)
    # Go must validate ownership before passing this context. No arbitrary browser payload.
    messageContext: str | None = Field(default=None, max_length=4000)


class Citation(StrictModel):
    chunkId: str
    documentId: str
    revision: str
    title: str
    section: str
    url: str
    synthetic: bool


class Hit(Citation):
    text: str
    score: float


class SearchResult(StrictModel):
    snapshotId: str | None = None
    retrievalMode: Literal["hybrid", "lexical_degraded"] = "hybrid"
    items: list[Hit] = []


class AnswerResult(StrictModel):
    requestId: str
    status: Literal["answered", "no_evidence", "conflicting_sources", "unavailable"]
    reply: str
    citations: list[Citation] = []
    snapshotId: str | None = None
    retrievalMode: Literal["hybrid", "lexical_degraded"] = "hybrid"


class Document(StrictModel):
    documentId: str = Field(pattern=r"^[a-zA-Z0-9_-]{1,48}$")
    revision: str = Field(min_length=1, max_length=80)
    title: str
    text: str
    url: str
    scope: Literal["common", "team", "internship"] = "common"
    scopeId: str | None = None
    synthetic: bool = False
    validFrom: datetime
    expiresAt: datetime

    @model_validator(mode="after")
    def valid_metadata(self):
        if not self.validFrom.tzinfo or not self.expiresAt.tzinfo:
            raise ValueError("validity timestamps must have timezone information")
        if self.expiresAt <= self.validFrom:
            raise ValueError("expiresAt must follow validFrom")
        if (self.scope == "common") != (self.scopeId is None):
            raise ValueError("common has no scopeId; team/internship require scopeId")
        if self.scopeId is not None and not self.scopeId.strip():
            raise ValueError("scopeId must not be blank")
        return self

    @field_validator("url")
    @classmethod
    def https_url(cls, value: str) -> str:
        from urllib.parse import urlparse

        parsed = urlparse(value)
        if parsed.scheme != "https" or not parsed.netloc or parsed.username or parsed.password:
            raise ValueError("source URL must be HTTPS without credentials")
        return value


class Chunk(StrictModel):
    chunkId: str
    section: str
    text: str
    ordinal: int
