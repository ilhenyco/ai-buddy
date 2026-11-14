"""Optional local demo page. The service credential stays on the server."""

import argparse
import time
from contextlib import asynccontextmanager
from html import escape
from pathlib import Path
from uuid import uuid4

import httpx
from fastapi import FastAPI, HTTPException, Request
from fastapi.responses import HTMLResponse
from pydantic import Field, model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict

from .models import AnswerResult, StrictModel


class DemoSettings(BaseSettings):
    model_config = SettingsConfigDict(env_prefix="RAG_DEMO_", extra="ignore", populate_by_name=True)

    service_token: str = Field(min_length=24, validation_alias="RAG_SERVICE_TOKEN")
    mode: bool = Field(default=False, validation_alias="RAG_DEMO_MODE")
    api_url: str = "http://127.0.0.1:8090"
    subject_id: str = Field(default="u-sasha", min_length=1, max_length=80)
    port: int = Field(default=8092, ge=1, le=65535)
    public_origins: list[str] = []
    timeout: float = Field(default=250, gt=0)

    @model_validator(mode="after")
    def demo_only(self):
        if not self.mode:
            raise ValueError("The test page requires RAG_DEMO_MODE=true")
        if not self.public_origins:
            self.public_origins = [f"http://127.0.0.1:{self.port}", f"http://localhost:{self.port}"]
        return self


class Question(StrictModel):
    query: str = Field(min_length=1, max_length=1000)


def create_demo_app(settings: DemoSettings, client: httpx.AsyncClient | None = None) -> FastAPI:
    owns_client = client is None
    upstream = client or httpx.AsyncClient(timeout=settings.timeout)
    page = Path(__file__).with_name("demo.html").read_text(encoding="utf-8")
    page = page.replace("{{subject}}", escape(settings.subject_id))

    @asynccontextmanager
    async def lifespan(app):
        yield
        if owns_client:
            await upstream.aclose()

    app = FastAPI(lifespan=lifespan, docs_url=None, redoc_url=None, openapi_url=None)

    @app.get("/", response_class=HTMLResponse)
    async def index():
        return HTMLResponse(page, headers={"Cache-Control": "no-store"})

    @app.post("/ask")
    async def ask(body: Question, request: Request):
        if request.headers.get("origin") not in settings.public_origins:
            raise HTTPException(403, "Open the local test page to send a question")
        if not body.query.strip():
            raise HTTPException(422, "Question must not be blank")
        started = time.monotonic()
        try:
            response = await upstream.post(
                settings.api_url.rstrip("/") + "/internal/v1/answer",
                headers={"Authorization": f"Bearer {settings.service_token}"},
                json={
                    "subjectId": settings.subject_id,
                    "requestId": "demo-" + uuid4().hex,
                    "query": body.query.strip(),
                },
            )
            response.raise_for_status()
            answer = AnswerResult.model_validate(response.json())
        except (httpx.HTTPError, ValueError) as exc:
            raise HTTPException(
                503, "RAG is unavailable; check model installation and ingestion"
            ) from exc
        return {**answer.model_dump(), "seconds": round(time.monotonic() - started, 1)}

    return app


def main():
    import uvicorn

    parser = argparse.ArgumentParser(
        description="Local RAG experiment, separate from the product UI"
    )
    parser.add_argument("--env-file", help="Local dotenv file, e.g. ../.env")
    parser.add_argument("--host", choices=["127.0.0.1", "0.0.0.0"], default="127.0.0.1")
    parser.add_argument("--port", type=int, default=8092)
    args = parser.parse_args()
    settings = DemoSettings(_env_file=args.env_file, port=args.port)
    uvicorn.run(
        create_demo_app(settings),
        host=args.host,
        port=settings.port,
        access_log=False,
        loop="homych_knowledge.runtime:loop_factory",
    )


if __name__ == "__main__":
    main()
