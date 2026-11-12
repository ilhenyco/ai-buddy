import hashlib
import json
from pathlib import Path
from uuid import uuid4

import psycopg
from psycopg.rows import dict_row

from .chunking import chunk_document
from .models import AccessContext, Document, Hit, SearchResult

# Reused for ranking AND final citation validation. Never filter after a global top-k.
ALLOWED = """
SELECT c.* FROM rag.chunk c
JOIN rag.active a ON a.snapshot_id = c.snapshot_id
WHERE c.valid_from <= now() AND c.expires_at > now()
  AND NOT EXISTS (SELECT 1 FROM rag.revocation r WHERE r.document_id = c.document_id)
  AND (NOT c.synthetic OR %(synthetic)s)
  AND (c.scope = 'common'
       OR (c.scope = 'team' AND c.scope_id = ANY(%(teams)s))
       OR (c.scope = 'internship' AND c.scope_id = ANY(%(internships)s)))
"""


def acl_params(access: AccessContext) -> dict:
    return {
        "teams": access.teamIds,
        "internships": access.internshipIds,
        "synthetic": access.synthetic,
    }


def vector_literal(vector: list[float]) -> str:
    return "[" + ",".join(str(x) for x in vector) + "]"


def hit(row: dict) -> Hit:
    return Hit(
        chunkId=row["chunk_id"],
        documentId=row["document_id"],
        revision=row["revision"],
        title=row["title"],
        section=row["section"],
        url=row["url"],
        synthetic=row["synthetic"],
        text=row["body"],
        score=row["score"],
    )


class Store:
    def __init__(self, database_url: str):
        self.database_url = database_url

    async def connect(self):
        return await psycopg.AsyncConnection.connect(
            self.database_url,
            row_factory=dict_row,
            connect_timeout=5,
            options="-c statement_timeout=10000",
        )

    async def migrate(self):
        sql = Path(__file__).with_name("schema.sql").read_text(encoding="utf-8")
        async with await self.connect() as conn:
            await conn.execute("SELECT pg_advisory_xact_lock(8213641)")
            await conn.execute(sql)

    async def active_digest(self) -> str | None:
        async with await self.connect() as conn:
            row = await (
                await conn.execute(
                    "SELECT s.embedding_digest FROM rag.snapshot s "
                    "JOIN rag.active a ON a.snapshot_id=s.id"
                )
            ).fetchone()
            return row["embedding_digest"] if row else None

    async def search(
        self,
        query: str,
        access: AccessContext,
        vector: list[float] | None,
        limit: int,
        threshold: float,
        digest: str | None = None,
    ) -> SearchResult:
        params = {
            **acl_params(access),
            "query": query,
            "threshold": threshold,
            "vector": vector_literal(vector) if vector else None,
        }
        sql = (
            """
        WITH allowed AS MATERIALIZED ("""
            + ALLOWED
            + """),
        queries AS (
          SELECT websearch_to_tsquery('russian', %(query)s) ru,
                 websearch_to_tsquery('simple', %(query)s) simple
        ), lexical_scores AS (
          SELECT c.chunk_id,
                 ts_rank_cd(c.fts_ru,q.ru) + ts_rank_cd(c.fts_simple,q.simple) score
          FROM allowed c CROSS JOIN queries q
          WHERE c.fts_ru @@ q.ru OR c.fts_simple @@ q.simple
        ), lex AS (
          SELECT chunk_id, row_number() OVER (ORDER BY score DESC, chunk_id) rank
          FROM lexical_scores ORDER BY score DESC, chunk_id LIMIT 20
        ), vec AS (
          SELECT chunk_id, row_number() OVER (
              ORDER BY embedding <=> %(vector)s::vector, chunk_id) rank
          FROM allowed WHERE %(vector)s::vector IS NOT NULL
            AND 1 - (embedding <=> %(vector)s::vector) >= %(threshold)s
          ORDER BY embedding <=> %(vector)s::vector, chunk_id LIMIT 20
        ), fused AS (
          SELECT COALESCE(l.chunk_id,v.chunk_id) chunk_id,
                 COALESCE(1.0/(60+l.rank),0) + COALESCE(1.0/(60+v.rank),0) score
          FROM lex l FULL JOIN vec v ON l.chunk_id=v.chunk_id
        )
        SELECT c.*, f.score::float FROM fused f JOIN allowed c USING (chunk_id)
        ORDER BY f.score DESC, c.chunk_id
        """
        )
        async with await self.connect() as conn:
            # One transaction view for snapshot metadata and both ranking channels.
            await conn.execute("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ")
            active = await (
                await conn.execute(
                    "SELECT snapshot_id, embedding_digest FROM rag.active a "
                    "JOIN rag.snapshot s ON s.id=a.snapshot_id"
                )
            ).fetchone()
            if not active or active["embedding_digest"] != digest:
                params["vector"] = None
            rows = await (await conn.execute(sql, params)).fetchall()
        counts: dict[str, int] = {}
        items = []
        for row in rows:
            key = row["document_id"]
            if counts.get(key, 0) < 2:
                items.append(hit(row))
                counts[key] = counts.get(key, 0) + 1
            if len(items) == limit:
                break
        return SearchResult(
            snapshotId=active["snapshot_id"] if active else None,
            retrievalMode="hybrid" if params["vector"] else "lexical_degraded",
            items=items,
        )

    async def permitted(
        self, access: AccessContext, snapshot_id: str | None, chunk_ids: list[str]
    ) -> bool:
        if not snapshot_id or not chunk_ids:
            return False
        sql = (
            "SELECT chunk_id FROM (" + ALLOWED + ") c WHERE "
            "snapshot_id=%(snapshot)s AND chunk_id = ANY(%(ids)s)"
        )
        async with await self.connect() as conn:
            rows = await (
                await conn.execute(
                    sql, {**acl_params(access), "snapshot": snapshot_id, "ids": chunk_ids}
                )
            ).fetchall()
        return {r["chunk_id"] for r in rows} == set(chunk_ids)

    async def publish(self, documents: list[Document], provider) -> str:
        if not documents:
            raise ValueError("refusing to publish an empty corpus")
        digest = await provider.identity()
        prepared = []
        seen = set()
        for doc in documents:
            if doc.documentId in seen:
                raise ValueError("duplicate document ID")
            seen.add(doc.documentId)
            if (doc.scope == "common") != (doc.scopeId is None):
                raise ValueError("invalid scope metadata")
            if doc.expiresAt <= doc.validFrom:
                raise ValueError("invalid validity range")
            chunks = chunk_document(doc)
            if not chunks:
                raise ValueError("empty document")
            # Reuse only exact embedding inputs produced by the same model digest.
            async with await self.connect() as conn:
                rows = await (
                    await conn.execute(
                        "SELECT c.section,c.body,c.embedding::text FROM rag.chunk c "
                        "JOIN rag.snapshot s ON s.id=c.snapshot_id "
                        "WHERE c.document_id=%s AND c.title=%s AND s.embedding_digest=%s",
                        (doc.documentId, doc.title, digest),
                    )
                ).fetchall()
            cache = {(row["section"], row["body"]): json.loads(row["embedding"]) for row in rows}
            embeddings = [cache.get((c.section, c.text)) for c in chunks]
            missing = [n for n, vector in enumerate(embeddings) if vector is None]
            for start in range(0, len(missing), 8):
                indices = missing[start : start + 8]
                vectors = await provider.embed(
                    [f"{doc.title}\n{chunks[n].section}\n{chunks[n].text}" for n in indices]
                )
                for index, vector in zip(indices, vectors, strict=True):
                    embeddings[index] = vector
            prepared.append((doc, chunks, embeddings))
        if await provider.identity() != digest:
            raise ValueError("embedding model changed during ingestion")
        snapshot = "snap-" + uuid4().hex
        async with await self.connect() as conn:
            await conn.execute("SELECT pg_advisory_xact_lock(8213642)")
            await conn.execute(
                "INSERT INTO rag.snapshot(id,embedding_digest) VALUES (%s,%s)", (snapshot, digest)
            )
            for doc, chunks, embeddings in prepared:
                # Include ACL and provenance in immutable revision identity.
                content_hash = hashlib.sha256(
                    json.dumps(
                        doc.model_dump(mode="json"), sort_keys=True, ensure_ascii=False
                    ).encode()
                ).hexdigest()
                existing = await (
                    await conn.execute(
                        "SELECT content_hash FROM rag.document_revision "
                        "WHERE document_id=%s AND revision=%s",
                        (doc.documentId, doc.revision),
                    )
                ).fetchone()
                if existing and existing["content_hash"] != content_hash:
                    raise ValueError("revision already exists with different content or ACL")
                await conn.execute(
                    "INSERT INTO rag.document_revision VALUES (%s,%s,%s) ON CONFLICT DO NOTHING",
                    (doc.documentId, doc.revision, content_hash),
                )
                for chunk, embedding in zip(chunks, embeddings, strict=True):
                    await conn.execute(
                        """
                    INSERT INTO rag.chunk VALUES (
                      %s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s::vector,
                      setweight(to_tsvector('russian',%s),'A') ||
                      setweight(to_tsvector('russian',%s),'B') ||
                      setweight(to_tsvector('russian',%s),'C'),
                      setweight(to_tsvector('simple',%s),'A') ||
                      setweight(to_tsvector('simple',%s),'B') ||
                      setweight(to_tsvector('simple',%s),'C'))
                    """,
                        (
                            snapshot,
                            chunk.chunkId,
                            doc.documentId,
                            doc.revision,
                            doc.title,
                            chunk.section,
                            chunk.text,
                            doc.url,
                            doc.scope,
                            doc.scopeId,
                            doc.synthetic,
                            doc.validFrom,
                            doc.expiresAt,
                            vector_literal(embedding),
                            doc.title,
                            chunk.section,
                            chunk.text,
                            doc.title,
                            chunk.section,
                            chunk.text,
                        ),
                    )
            await conn.execute(
                "INSERT INTO rag.active VALUES (true,%s) ON CONFLICT (singleton) "
                "DO UPDATE SET snapshot_id=excluded.snapshot_id",
                (snapshot,),
            )
        return snapshot

    async def revoke(self, document_id: str):
        async with await self.connect() as conn:
            await conn.execute(
                "INSERT INTO rag.revocation(document_id) VALUES (%s) ON CONFLICT DO NOTHING",
                (document_id,),
            )
