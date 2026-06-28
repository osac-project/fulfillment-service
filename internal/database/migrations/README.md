# Database Migrations

## Adding a new migration

1. Generate a timestamp prefix:
   ```bash
   date +%Y%m%d%H%M%S
   ```
2. Create your migration file(s):
   - `<timestamp>_<description>.up.sql` (required)
   - `<timestamp>_<description>.down.sql` (optional)
3. Description must be `snake_case`, starting with a verb (`add_`, `create_`, `drop_`, `rename_`, `migrate_`).

Example:
```bash
# Generate prefix
$ date +%Y%m%d%H%M%S
20260625143022

# Create the file
$ touch internal/database/migrations/20260625143022_add_storage_class_column.up.sql
```

## Why timestamps?

With sequential numbering (`64_`, `65_`, ...), two developers
independently adding migrations will both pick the same next number.
This forces one of them to renumber after the other's PR merges.

Timestamp prefixes (`YYYYMMDDHHMMSS`) eliminate this problem — two
developers would need to generate the prefix at the exact same second
to collide. This is the same approach used by
[assisted-service](https://github.com/openshift/assisted-service/tree/master/internal/migrations).

## Legacy migrations

Migrations 0–63 use the original sequential numbering. They are kept
as-is to avoid breaking deployed databases that track the current
version in `schema_migrations`. Since `20260625... > 63`, golang-migrate
orders them correctly after the legacy files.

## CI enforcement

The unit test in `database_migrations_test.go` enforces that any
migration with a number > 63 uses the 14-digit timestamp format. PRs
with incorrectly named migration files will fail CI.
