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
-- One organization per deployment. Writer credentials administer the entire corpus.
CREATE TABLE IF NOT EXISTS rag.corpus_document (
    document_id text PRIMARY KEY,
    payload jsonb NOT NULL
);
CREATE TABLE IF NOT EXISTS rag.ingestion_job (
    id text PRIMARY KEY,
    client_id text NOT NULL,
    request_key text NOT NULL,
    document_id text NOT NULL,
    payload jsonb,
    status text NOT NULL DEFAULT 'pending',
    attempts integer NOT NULL DEFAULT 0,
    error_code text,
    created_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    UNIQUE (client_id, request_key)
);
CREATE INDEX IF NOT EXISTS ingestion_pending ON rag.ingestion_job(created_at) WHERE status <> 'completed';
CREATE INDEX IF NOT EXISTS ingestion_hidden_document ON rag.ingestion_job(document_id)
    WHERE status <> 'completed';
