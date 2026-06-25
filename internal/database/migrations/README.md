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

Sequential numbering (`54_`, `55_`, ...) caused collisions when two PRs
independently claimed the same next number. With `strict: false` on
branch protection (required to avoid tide retest loops), both PRs could
merge without git detecting a conflict, breaking the service at startup.

Timestamp prefixes (`YYYYMMDDHHMMSS`) make collisions practically
impossible — two developers would need to create a migration at the
exact same second.

## Legacy migrations

Migrations 0–63 use the original sequential numbering. They are kept
as-is to avoid breaking deployed databases that track the current
version in `schema_migrations`. Since `20260625... > 63`, golang-migrate
orders them correctly after the legacy files.

## CI enforcement

The unit test in `database_migrations_test.go` enforces that any
migration with a number > 63 uses the 14-digit timestamp format. PRs
with incorrectly named migration files will fail CI.
