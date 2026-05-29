# Milvus Native BM25 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace local Go BM25 retrieval with Milvus native BM25 sparse retrieval and hybrid search.

**Architecture:** Upgrade the Milvus client dependency to the 2.6+ client module, create a collection with dense and BM25-generated sparse vector fields, and query through Milvus hybrid search. Keep the existing RAG API response shape and post-level deduplication.

**Tech Stack:** Go 1.25 toolchain, Milvus 2.6+ Docker image, `github.com/milvus-io/milvus/client/v2`, Gin, existing embedding provider.

---

### Task 1: Upgrade Milvus Runtime And Client

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `README.md`

- [ ] Document that Milvus may run separately in a VM Docker environment and that Windows config should use the VM IP.
- [ ] Add `github.com/milvus-io/milvus/client/v2` to `go.mod`.
- [ ] Document that existing dense-only collections must be replaced with a new hybrid collection name and reindexed.
- [ ] Run `go mod tidy`.

### Task 2: Migrate Milvus DAO Schema

**Files:**
- Modify: `internal/dao/milvus/milvus.go`

- [ ] Add a `sparse_embedding` field constant.
- [ ] Create `chunk_text` with analyzer enabled.
- [ ] Create a BM25 function from `chunk_text` to `sparse_embedding`.
- [ ] Create a dense index for `embedding`.
- [ ] Create a sparse inverted index for `sparse_embedding` using BM25 metric.
- [ ] Validate that existing collections include `sparse_embedding`; otherwise fail fast.

### Task 3: Replace Dense Search With Hybrid Search

**Files:**
- Modify: `internal/dao/milvus/milvus.go`
- Modify: `internal/logic/rag.go`
- Delete: local BM25 cache code if no longer used.

- [ ] Replace `SearchByVector` with a hybrid search function that accepts both dense vector and raw query text.
- [ ] Use RRF ranking inside Milvus.
- [ ] Keep `SearchHit` unchanged for upper layers.
- [ ] Deduplicate hits by `post_id`.
- [ ] Remove Go local BM25 search, cache, invalidation, and tests that only apply to local BM25.

### Task 4: Verify

**Files:**
- Modify/add focused tests where SDK-independent behavior exists.

- [ ] Run `go test ./...`.
- [ ] Run manual Milvus verification against VM Docker Milvus 2.6+: start Milvus, rebuild collection, run RAG reindex, run `/api/v1/rag/search`.
