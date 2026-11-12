CREATE EXTENSION IF NOT EXISTS vector;
CREATE SCHEMA IF NOT EXISTS rag;
CREATE TABLE IF NOT EXISTS rag.snapshot (
    id text PRIMARY KEY,
    embedding_digest text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS rag.active (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    snapshot_id text NOT NULL REFERENCES rag.snapshot(id)
);
CREATE TABLE IF NOT EXISTS rag.document_revision (
    document_id text NOT NULL,
    revision text NOT NULL,
    content_hash text NOT NULL,
    PRIMARY KEY (document_id, revision)
);
CREATE TABLE IF NOT EXISTS rag.chunk (
    snapshot_id text NOT NULL REFERENCES rag.snapshot(id),
    chunk_id text NOT NULL,
    document_id text NOT NULL,
    revision text NOT NULL,
    title text NOT NULL,
    section text NOT NULL,
    body text NOT NULL,
    url text NOT NULL,
    scope text NOT NULL CHECK (scope IN ('common', 'team', 'internship')),
    scope_id text,
    synthetic boolean NOT NULL,
    valid_from timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    embedding vector(1024) NOT NULL,
    fts_ru tsvector NOT NULL,
    fts_simple tsvector NOT NULL,
    PRIMARY KEY (snapshot_id, chunk_id),
    FOREIGN KEY (document_id, revision) REFERENCES rag.document_revision,
    CHECK ((scope = 'common' AND scope_id IS NULL) OR
           (scope <> 'common' AND scope_id IS NOT NULL)),
    CHECK (expires_at > valid_from)
);
CREATE TABLE IF NOT EXISTS rag.revocation (
    document_id text PRIMARY KEY,
    revoked_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS chunk_fts_ru ON rag.chunk USING gin(fts_ru);
CREATE INDEX IF NOT EXISTS chunk_fts_simple ON rag.chunk USING gin(fts_simple);
