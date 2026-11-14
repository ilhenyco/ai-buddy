"""External API: private bearer-key profile, single organization per deployment."""

import asyncio
import hashlib
import json
from uuid import uuid4

from fastapi import HTTPException, Request
from pydantic import Field, ValidationError

from .models import Document, SearchResult, StrictModel


async def enqueue(store, client_id, key, document_id, payload):
    if not key or len(key) > 80:
        raise HTTPException(422, "Idempotency-Key required (1..80 characters)")
    async with await store.connect() as conn:
        # Serialize enqueue with itself; identical retries return the same receipt.
        await conn.execute("SELECT pg_advisory_xact_lock(8213643)")
        existing = await (
            await conn.execute(
                "SELECT * FROM rag.ingestion_job WHERE client_id=%s AND request_key=%s",
                (client_id, key),
            )
        ).fetchone()
        if existing:
            if existing["document_id"] != document_id or existing["payload"] != payload:
                raise HTTPException(409, "Idempotency key reused with different payload")
            return {"jobId": existing["id"], "status": existing["status"]}
        if payload:
            fingerprint = hashlib.sha256(
                json.dumps(payload, sort_keys=True, ensure_ascii=False).encode()
            ).hexdigest()
            revision = await (
                await conn.execute(
                    "SELECT content_hash FROM rag.document_revision "
                    "WHERE document_id=%s AND revision=%s",
                    (document_id, payload["revision"]),
                )
            ).fetchone()
            pending = await (
                await conn.execute(
                    "SELECT payload FROM rag.ingestion_job WHERE document_id=%s "
                    "AND status <> 'completed' AND payload->>'revision'=%s",
                    (document_id, payload["revision"]),
                )
            ).fetchall()
            if (revision and revision["content_hash"] != fingerprint) or any(
                row["payload"] != payload for row in pending
            ):
                raise HTTPException(409, "Revision is immutable; use a new revision")
        job_id = "job-" + uuid4().hex
        await conn.execute(
            "INSERT INTO rag.ingestion_job(id,client_id,request_key,document_id,payload) "
            "VALUES (%s,%s,%s,%s,%s::jsonb)",
            (job_id, client_id, key, document_id, json.dumps(payload) if payload else None),
        )
        return {"jobId": job_id, "status": "pending"}


class Query(StrictModel):
    query: str = Field(min_length=1, max_length=1000)
    limit: int = Field(default=5, ge=1, le=5)


def register_routes(app, store, service, settings):
    @app.post("/v1/search", response_model=SearchResult)
    async def search(body: Query, request: Request):
        if not body.query.strip():
            raise HTTPException(422, "Query must not be blank")
        async with asyncio.timeout(settings.request_timeout):
            return await service.search(
                request.scope["mcp_subject"], body.query.strip(), body.limit
            )

    @app.put(
        "/v1/documents/{document_id}",
        status_code=202,
        openapi_extra={
            "requestBody": {
                "required": True,
                "content": {
                    "application/json": {
                        "schema": Document.model_json_schema(),
                    }
                },
            },
        },
    )
    async def upsert(document_id: str, request: Request):
        body = bytearray()
        async for part in request.stream():
            body.extend(part)
            if len(body) > 1_000_000:
                raise HTTPException(413, "Document exceeds 1 MB")
        try:
            doc = Document.model_validate_json(bytes(body))
        except ValidationError as exc:
            raise HTTPException(422, "Invalid document schema") from exc
        if doc.documentId != document_id or not doc.text.strip() or doc.synthetic:
            raise HTTPException(422, "ID mismatch, empty text or synthetic source")
        return await enqueue(
            store,
            request.scope["client_id"],
            request.headers.get("Idempotency-Key"),
            document_id,
            doc.model_dump(mode="json"),
        )

    @app.delete("/v1/documents/{document_id}", status_code=202)
    async def delete(document_id: str, request: Request):
        return await enqueue(
            store,
            request.scope["client_id"],
            request.headers.get("Idempotency-Key"),
            document_id,
            None,
        )

    @app.get("/v1/jobs/{job_id}")
    async def job(job_id: str, request: Request):
        async with await store.connect() as conn:
            row = await (
                await conn.execute(
                    "SELECT id,status,attempts,error_code,created_at,finished_at "
                    "FROM rag.ingestion_job WHERE id=%s AND client_id=%s",
                    (job_id, request.scope["client_id"]),
                )
            ).fetchone()
        if not row:
            raise HTTPException(404, "Job not found")
        return row

    @app.post("/v1/jobs/{job_id}/retry", status_code=202)
    async def retry(job_id: str, request: Request):
        async with await store.connect() as conn:
            row = await (
                await conn.execute(
                    "UPDATE rag.ingestion_job SET attempts=0,status='pending',error_code=NULL "
                    "WHERE id=%s AND client_id=%s AND status='failed' RETURNING id",
                    (job_id, request.scope["client_id"]),
                )
            ).fetchone()
        if not row:
            raise HTTPException(409, "No retryable job for this client")
        return {"jobId": row["id"], "status": "pending"}
