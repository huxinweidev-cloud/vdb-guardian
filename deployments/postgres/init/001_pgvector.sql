-- Local-only pgvector initialization for the vdb-guardian migration stack.
-- This file is mounted into the PostgreSQL container by Docker Compose and must
-- not contain production credentials or customer data.
CREATE EXTENSION IF NOT EXISTS vector;
