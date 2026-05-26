-- Phase 6 ai-u5: semantic catalog search via embeddings.
--
-- Stores raw []float32 vectors packed as little-endian bytea blobs so we can
-- avoid the pgvector extension dependency in Phase 1. Cosine similarity is
-- computed in Go (internal/ai/embeddings.go). When customer counts grow we
-- swap this for pgvector + an ivfflat index — schema migration only, no API
-- shape change.

CREATE TABLE catalog_embedding (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id  UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    fqn          TEXT NOT NULL,
    kind         TEXT NOT NULL CHECK (kind IN ('table','column','dashboard','query')),
    text         TEXT NOT NULL,
    embedding    BYTEA NOT NULL,
    model        TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (customer_id, fqn, kind, model)
);
CREATE INDEX idx_catalog_embedding_customer ON catalog_embedding(customer_id, kind);
