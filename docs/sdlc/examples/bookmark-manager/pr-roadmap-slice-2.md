# Bookmark CRUD — PR Roadmap

> Decomposition of Slice 2 (Bookmark CRUD) into ordered PRs.

## Dependency graph

```
PR-01: Bookmark Model + Migration
    │
    ▼
PR-02: Bookmark Repository
    │
    ▼
PR-03: Bookmark Service
    │
    ▼
PR-04: Bookmark API Routes
```

Linear chain — each PR builds on the previous.

## PRs

### PR-01: Bookmark Model and Migration
- **Goal**: Add Bookmark type and create the bookmarks table
- **Dependencies**: None (Slice 1 complete — users table exists)
- **Acceptance**: Migration runs, types compile, row mapper tested

### PR-02: Bookmark Repository
- **Goal**: Add database queries for CRUD operations
- **Dependencies**: PR-01
- **Acceptance**: All queries work against test database

### PR-03: Bookmark Service
- **Goal**: Add business logic with validation and ownership checks
- **Dependencies**: PR-02
- **Acceptance**: Service functions handle all error cases from slice spec

### PR-04: Bookmark API Routes
- **Goal**: Add REST endpoints wired to service layer
- **Dependencies**: PR-03
- **Acceptance**: All endpoints return correct status codes, integration tests pass

## Strategy

Layered decomposition (types → storage → logic → API). Each PR only depends on the one before it. Each is independently testable and mergeable.
