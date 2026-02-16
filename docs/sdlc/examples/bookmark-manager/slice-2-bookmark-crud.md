# Slice 2 Spec: Bookmark CRUD

## 1. Goal & Scope

**Goal**: Users can save and manage bookmarks.

**In Scope**:
- POST /bookmarks — create bookmark
- GET /bookmarks — list bookmarks (paginated)
- GET /bookmarks/:id — get single bookmark
- PUT /bookmarks/:id — update bookmark
- DELETE /bookmarks/:id — delete bookmark
- bookmarks table in PostgreSQL
- Input validation

**Out of Scope**:
- Tags (Slice 3)
- Search/filtering (Slice 4)
- Import/export (Slice 5)

---

## 2. Domain Models

### Bookmark

| Field | Type | Constraints |
|-------|------|-------------|
| id | UUID | Primary key, auto-generated |
| user_id | UUID | Foreign key to users, not null |
| url | TEXT | Not null, valid URL, max 2048 chars |
| title | TEXT | Not null, 1-500 chars |
| description | TEXT | Nullable, max 2000 chars |
| created_at | TIMESTAMPTZ | Not null, auto-set |
| updated_at | TIMESTAMPTZ | Not null, auto-updated |

Note: `tags` added in Slice 3.

---

## 3. Database Schema

```sql
CREATE TABLE bookmarks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    url TEXT NOT NULL CHECK(length(url) <= 2048),
    title TEXT NOT NULL CHECK(length(title) >= 1 AND length(title) <= 500),
    description TEXT CHECK(description IS NULL OR length(description) <= 2000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, url)
);

CREATE INDEX idx_bookmarks_user_id ON bookmarks(user_id);
CREATE INDEX idx_bookmarks_created_at ON bookmarks(created_at DESC);
```

---

## 4. API Endpoints

### POST /api/v1/bookmarks

Create a new bookmark.

**Auth**: Required (JWT)

**Request Body**:
```json
{
  "url": "https://example.com",
  "title": "Example Site",
  "description": "A great site"
}
```

- `url`: required, valid http/https URL
- `title`: required, 1-500 chars
- `description`: optional, max 2000 chars

**Response 201**:
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "url": "https://example.com",
  "title": "Example Site",
  "description": "A great site",
  "createdAt": "2024-01-15T10:30:00Z",
  "updatedAt": "2024-01-15T10:30:00Z"
}
```

**Errors**:

| Code | Status | When |
|------|--------|------|
| E_VALIDATION_ERROR | 400 | Missing/invalid fields |
| E_URL_INVALID | 400 | URL not parseable or not http/https |
| E_DUPLICATE_URL | 409 | User already has bookmark with this URL |
| E_UNAUTHORIZED | 401 | Missing or invalid token |

---

### GET /api/v1/bookmarks

List user's bookmarks (paginated).

**Auth**: Required (JWT)

**Query Parameters**:

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| page | int | 1 | Page number (1-indexed) |
| limit | int | 20 | Items per page (max 100) |

**Response 200**:
```json
{
  "bookmarks": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "url": "https://example.com",
      "title": "Example Site",
      "description": "A great site",
      "createdAt": "2024-01-15T10:30:00Z",
      "updatedAt": "2024-01-15T10:30:00Z"
    }
  ],
  "pagination": {
    "page": 1,
    "limit": 20,
    "total": 1,
    "totalPages": 1
  }
}
```

**Behavior**:
- Only returns bookmarks owned by authenticated user
- Ordered by created_at DESC (newest first)
- Empty array if no bookmarks

**Errors**:

| Code | Status | When |
|------|--------|------|
| E_UNAUTHORIZED | 401 | Missing or invalid token |

---

### GET /api/v1/bookmarks/:id

Get a single bookmark.

**Auth**: Required (JWT)

**Response 200**: Bookmark object (same shape as list items)

**Errors**:

| Code | Status | When |
|------|--------|------|
| E_UNAUTHORIZED | 401 | Missing or invalid token |
| E_NOT_FOUND | 404 | Bookmark doesn't exist OR belongs to another user |

Note: Return 404 (not 403) when bookmark belongs to another user to avoid leaking existence.

---

### PUT /api/v1/bookmarks/:id

Update a bookmark.

**Auth**: Required (JWT)

**Request Body** (all fields optional):
```json
{
  "url": "https://updated.com",
  "title": "Updated Title",
  "description": "Updated description"
}
```

**Response 200**: Updated bookmark object (same as GET)

**Behavior**:
- Only provided fields are updated
- updated_at is auto-set to NOW()

**Errors**:

| Code | Status | When |
|------|--------|------|
| E_VALIDATION_ERROR | 400 | Invalid field values |
| E_URL_INVALID | 400 | URL not parseable or not http/https |
| E_DUPLICATE_URL | 409 | New URL already exists for this user |
| E_UNAUTHORIZED | 401 | Missing or invalid token |
| E_NOT_FOUND | 404 | Bookmark doesn't exist or not owned |

---

### DELETE /api/v1/bookmarks/:id

Delete a bookmark.

**Auth**: Required (JWT)

**Response 204**: No content

**Errors**:

| Code | Status | When |
|------|--------|------|
| E_UNAUTHORIZED | 401 | Missing or invalid token |
| E_NOT_FOUND | 404 | Bookmark doesn't exist or not owned |

---

## 5. Service Functions

```typescript
// src/services/bookmark.service.ts

interface CreateBookmarkInput {
  userId: string;
  url: string;
  title: string;
  description?: string;
}

interface UpdateBookmarkInput {
  url?: string;
  title?: string;
  description?: string | null;  // null to clear
}

interface PaginationOptions {
  page: number;
  limit: number;
}

interface PaginatedResult<T> {
  items: T[];
  total: number;
  page: number;
  limit: number;
  totalPages: number;
}

// Create a new bookmark
async function createBookmark(input: CreateBookmarkInput): Promise<Bookmark>;

// List bookmarks for a user
async function listBookmarks(
  userId: string,
  options: PaginationOptions
): Promise<PaginatedResult<Bookmark>>;

// Get a single bookmark (throws if not found or not owned)
async function getBookmark(userId: string, bookmarkId: string): Promise<Bookmark>;

// Update a bookmark (throws if not found or not owned)
async function updateBookmark(
  userId: string,
  bookmarkId: string,
  input: UpdateBookmarkInput
): Promise<Bookmark>;

// Delete a bookmark (throws if not found or not owned)
async function deleteBookmark(userId: string, bookmarkId: string): Promise<void>;
```

---

## 6. Error Codes

| Code | Message | HTTP |
|------|---------|------|
| E_VALIDATION_ERROR | "Validation failed: {details}" | 400 |
| E_URL_INVALID | "Invalid URL: must be a valid http or https URL" | 400 |
| E_TITLE_EMPTY | "Title cannot be empty" | 400 |
| E_TITLE_TOO_LONG | "Title cannot exceed 500 characters" | 400 |
| E_DESCRIPTION_TOO_LONG | "Description cannot exceed 2000 characters" | 400 |
| E_DUPLICATE_URL | "A bookmark with this URL already exists" | 409 |
| E_NOT_FOUND | "Bookmark not found" | 404 |
| E_UNAUTHORIZED | "Authentication required" | 401 |

---

## 7. Invariants (This Slice)

1. A bookmark MUST belong to exactly one user
2. A user CANNOT have two bookmarks with the same URL
3. All bookmark URLs MUST be valid http:// or https:// URLs
4. getBookmark/updateBookmark/deleteBookmark MUST verify ownership
5. Listing bookmarks MUST only return bookmarks owned by the requesting user
6. Deleting a user MUST cascade delete all their bookmarks

---

## 8. Acceptance Scenarios

**Scenario: Create and list bookmark**
```
Given: User is authenticated
When: POST /bookmarks with {"url": "https://github.com", "title": "GitHub"}
Then: Response is 201 with bookmark object including generated ID
When: GET /bookmarks
Then: Response includes the created bookmark
```

**Scenario: Update bookmark**
```
Given: Bookmark exists with title "Old Title"
When: PUT /bookmarks/:id with {"title": "New Title"}
Then: Response shows updated title
And: updatedAt is newer than createdAt
```

**Scenario: Delete bookmark**
```
Given: Bookmark exists
When: DELETE /bookmarks/:id
Then: Response is 204
When: GET /bookmarks/:id
Then: Response is 404
```

**Scenario: Cannot access other user's bookmark**
```
Given: User A has a bookmark
When: User B tries GET /bookmarks/:id (User A's bookmark)
Then: Response is 404 (not 403)
```

**Scenario: Duplicate URL rejected**
```
Given: User has bookmark with url "https://example.com"
When: POST /bookmarks with same URL
Then: Response is 409 E_DUPLICATE_URL
```

**Scenario: Invalid URL rejected**
```
Given: User is authenticated
When: POST /bookmarks with {"url": "not-a-url", "title": "Test"}
Then: Response is 400 E_URL_INVALID
```
