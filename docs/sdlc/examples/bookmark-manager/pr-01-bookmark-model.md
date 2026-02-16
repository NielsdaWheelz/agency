# PR-01: Bookmark Model and Database Migration

## Goal
Add the Bookmark type and create the PostgreSQL migration for the bookmarks table.

## Context
- Slice 2 spec defines the Bookmark model and schema
- Project structure from Slice 0 exists (Express app, db connection)
- Users table exists from Slice 1

## Dependencies
- Slice 1 must be complete (users table exists)

---

## Files to Create

### src/models/bookmark.model.ts

```typescript
/**
 * Bookmark entity representing a saved URL.
 */
export interface Bookmark {
  id: string;
  userId: string;
  url: string;
  title: string;
  description: string | null;
  createdAt: Date;
  updatedAt: Date;
}

/**
 * Input for creating a new bookmark.
 * userId is added by the service layer from auth context.
 */
export interface CreateBookmarkDto {
  url: string;
  title: string;
  description?: string;
}

/**
 * Input for updating a bookmark.
 * All fields optional — only provided fields are updated.
 */
export interface UpdateBookmarkDto {
  url?: string;
  title?: string;
  description?: string | null;
}

/**
 * Convert database row to Bookmark entity.
 * Handles snake_case → camelCase conversion.
 */
export function rowToBookmark(row: {
  id: string;
  user_id: string;
  url: string;
  title: string;
  description: string | null;
  created_at: Date;
  updated_at: Date;
}): Bookmark {
  return {
    id: row.id,
    userId: row.user_id,
    url: row.url,
    title: row.title,
    description: row.description,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  };
}
```

### src/db/migrations/002_create_bookmarks.sql

```sql
-- Migration: Create bookmarks table
-- Depends on: 001_create_users.sql

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

-- Trigger to auto-update updated_at
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_bookmarks_updated_at
    BEFORE UPDATE ON bookmarks
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
```

---

## Files to Modify

### src/models/index.ts

Add exports:
```typescript
export * from './bookmark.model';
```

---

## Test Specifications

### File: src/models/__tests__/bookmark.model.test.ts

```typescript
import { rowToBookmark } from '../bookmark.model';

describe('rowToBookmark', () => {
  it('converts snake_case row to camelCase Bookmark', () => {
    const row = {
      id: '550e8400-e29b-41d4-a716-446655440000',
      user_id: '660e8400-e29b-41d4-a716-446655440000',
      url: 'https://example.com',
      title: 'Example',
      description: 'A site',
      created_at: new Date('2024-01-15T10:30:00Z'),
      updated_at: new Date('2024-01-15T10:30:00Z'),
    };

    const bookmark = rowToBookmark(row);

    expect(bookmark.id).toBe(row.id);
    expect(bookmark.userId).toBe(row.user_id);
    expect(bookmark.url).toBe(row.url);
    expect(bookmark.title).toBe(row.title);
    expect(bookmark.description).toBe(row.description);
    expect(bookmark.createdAt).toEqual(row.created_at);
    expect(bookmark.updatedAt).toEqual(row.updated_at);
  });

  it('handles null description', () => {
    const row = {
      id: '550e8400-e29b-41d4-a716-446655440000',
      user_id: '660e8400-e29b-41d4-a716-446655440000',
      url: 'https://example.com',
      title: 'Example',
      description: null,
      created_at: new Date('2024-01-15T10:30:00Z'),
      updated_at: new Date('2024-01-15T10:30:00Z'),
    };

    const bookmark = rowToBookmark(row);

    expect(bookmark.description).toBeNull();
  });
});
```

### File: src/db/__tests__/migrations.test.ts (add to existing)

```typescript
describe('002_create_bookmarks migration', () => {
  it('creates bookmarks table', async () => {
    const result = await db.query(`
      SELECT column_name, data_type, is_nullable
      FROM information_schema.columns
      WHERE table_name = 'bookmarks'
      ORDER BY ordinal_position
    `);

    expect(result.rows).toEqual([
      { column_name: 'id', data_type: 'uuid', is_nullable: 'NO' },
      { column_name: 'user_id', data_type: 'uuid', is_nullable: 'NO' },
      { column_name: 'url', data_type: 'text', is_nullable: 'NO' },
      { column_name: 'title', data_type: 'text', is_nullable: 'NO' },
      { column_name: 'description', data_type: 'text', is_nullable: 'YES' },
      { column_name: 'created_at', data_type: 'timestamp with time zone', is_nullable: 'NO' },
      { column_name: 'updated_at', data_type: 'timestamp with time zone', is_nullable: 'NO' },
    ]);
  });

  it('enforces unique constraint on user_id + url', async () => {
    const userId = await createTestUser();

    await db.query(`
      INSERT INTO bookmarks (user_id, url, title)
      VALUES ($1, 'https://example.com', 'First')
    `, [userId]);

    await expect(db.query(`
      INSERT INTO bookmarks (user_id, url, title)
      VALUES ($1, 'https://example.com', 'Duplicate')
    `, [userId])).rejects.toThrow(/unique/i);
  });

  it('cascades delete when user is deleted', async () => {
    const userId = await createTestUser();

    await db.query(`
      INSERT INTO bookmarks (user_id, url, title)
      VALUES ($1, 'https://example.com', 'Test')
    `, [userId]);

    await db.query('DELETE FROM users WHERE id = $1', [userId]);

    const result = await db.query(
      'SELECT COUNT(*) FROM bookmarks WHERE user_id = $1',
      [userId]
    );
    expect(result.rows[0].count).toBe('0');
  });

  it('enforces title length constraints', async () => {
    const userId = await createTestUser();

    // Empty title should fail
    await expect(db.query(`
      INSERT INTO bookmarks (user_id, url, title)
      VALUES ($1, 'https://example.com', '')
    `, [userId])).rejects.toThrow(/check/i);

    // Title > 500 chars should fail
    const longTitle = 'x'.repeat(501);
    await expect(db.query(`
      INSERT INTO bookmarks (user_id, url, title)
      VALUES ($1, 'https://example.com', $2)
    `, [userId, longTitle])).rejects.toThrow(/check/i);
  });

  it('auto-updates updated_at on UPDATE', async () => {
    const userId = await createTestUser();

    const insert = await db.query(`
      INSERT INTO bookmarks (user_id, url, title)
      VALUES ($1, 'https://example.com', 'Original')
      RETURNING updated_at
    `, [userId]);

    const originalUpdatedAt = insert.rows[0].updated_at;

    // Wait a moment to ensure timestamp differs
    await new Promise(r => setTimeout(r, 10));

    const update = await db.query(`
      UPDATE bookmarks SET title = 'Updated'
      WHERE user_id = $1
      RETURNING updated_at
    `, [userId]);

    expect(update.rows[0].updated_at.getTime())
      .toBeGreaterThan(originalUpdatedAt.getTime());
  });
});
```

---

## Non-Goals

- Does NOT implement bookmark service functions (PR-02)
- Does NOT implement API routes (PR-03)
- Does NOT implement input validation (PR-02)
- Does NOT implement bookmark repository/queries (PR-02)
- Does NOT add tags support (Slice 3)

---

## Constraints

- Only create/modify files listed above
- No new npm dependencies
- Migration must be idempotent (use IF NOT EXISTS where applicable)
- All TypeScript must pass strict type checking
- Follow existing naming conventions in the codebase

---

## Checklist

- [ ] npm run build succeeds with no errors
- [ ] npm run test passes (all new tests)
- [ ] npm run lint produces no warnings
- [ ] Migration runs successfully on fresh database
- [ ] Migration runs successfully on database with existing data
- [ ] Only listed files modified
