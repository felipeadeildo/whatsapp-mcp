# Contributing to WhatsApp MCP

Thanks for your interest in contributing! This guide will help you set up the development environment and understand the project structure.

## Project Structure

This project contains two separate Go programs:

```
.
├── main.go                    # Main WhatsApp MCP server
├── cmd/
│   └── migrate/
│       └── main.go           # Database migration CLI tool
├── storage/
│   ├── migrations/           # SQL migration files
│   ├── migrator.go          # Migration engine
│   └── db.go                # Database initialization
└── ...
```

### Why Two Main Programs?

- **`main.go`** - The primary application (WhatsApp MCP server)
- **`cmd/migrate/main.go`** - Standalone CLI tool for managing migrations

This follows Go's standard convention of placing separate commands in `cmd/` subdirectories.

## Building the Project

### Build the Server

```bash
go build -o whatsapp-mcp main.go
./whatsapp-mcp
```

Or run directly:
```bash
go run main.go
```

### Build the Migration Tool

```bash
go build -o migrate cmd/migrate/main.go
./migrate create my_migration
```

Or run directly:
```bash
go run cmd/migrate/main.go create my_migration
```

## Database Migrations

The project uses a custom migration system to manage database schema changes safely and automatically.

### How It Works

1. Migration files are stored in `storage/migrations/` as numbered SQL files
2. Each file has a version number: `001_initial_schema.sql`, `002_add_feature.sql`, etc.
3. On startup, the server automatically applies pending migrations in order
4. Applied migrations are tracked in the `schema_migrations` table
5. Checksums prevent accidental modifications to applied migrations

### Creating a New Migration

Use the migration CLI tool:

```bash
go run cmd/migrate/main.go create add_message_reactions
```

This will:
- Determine the next version number (e.g., `002`)
- Sanitize the description to `add_message_reactions`
- Create `storage/migrations/002_add_message_reactions.sql`
- Include a template with metadata and examples

### Migration File Template

```sql
-- Migration: 002_add_message_reactions
-- Description: add message reactions
-- Previous: 001
-- Version: 002
-- Created: 2026-01-04

-- Add your SQL statements below
-- Example:
-- CREATE TABLE IF NOT EXISTS example (
--     id INTEGER PRIMARY KEY,
--     name TEXT NOT NULL
-- );

-- Data transformation example:
-- UPDATE existing_table SET new_column = 'default_value' WHERE new_column IS NULL;

-- Create indexes:
-- CREATE INDEX IF NOT EXISTS idx_example_name ON example(name);
```

### Editing Migrations

1. Replace the example comments with your actual SQL statements
2. Use `IF NOT EXISTS` clauses to make migrations idempotent
3. Test your migration locally before committing
4. Never modify a migration file after it's been merged to main

### Applying Migrations

Migrations are applied automatically when you start the server:

```bash
go run main.go
```

You'll see output like:
```
INFO: Running migration 001_initial_schema.sql
INFO: Migration 001_initial_schema.sql applied successfully
INFO: Running migration 002_add_message_reactions.sql
INFO: Migration 002_add_message_reactions.sql applied successfully
INFO: All migrations applied successfully
```

### Best Practices

1. **One migration per feature** - Keep migrations focused and atomic
2. **Use transactions** - The migration system wraps each migration in a transaction
3. **Test locally first** - Run migrations on your local database before pushing
4. **Never modify applied migrations** - Create a new migration to fix issues
5. **Include indexes** - Add indexes for any new columns that will be queried
6. **Handle existing data** - Include UPDATE statements if you're adding NOT NULL columns

### Migration Commands

```bash
# Check migration status
go run cmd/migrate/main.go status

# Apply all pending migrations
go run cmd/migrate/main.go upgrade latest

# Apply migrations up to a specific version
go run cmd/migrate/main.go upgrade 2
```

## Development Workflow

1. **Fork and clone** the repository
2. **Create a branch** for your feature: `git checkout -b feat/my-feature`
3. **Make your changes** following the code style
4. **Test locally** with `go run main.go`
5. **Create migrations** if you changed the database schema
6. **Commit and push** your changes
7. **Open a pull request** with a clear description

## Code Style

- Follow standard Go conventions (gofmt, golint)
- Use meaningful variable and function names
- Add comments for exported functions and types
- Keep functions small and focused
- Handle errors explicitly

## Testing

```bash
# Run tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run a specific test
go test -run TestFunctionName ./...
```

## Docker Development

The project includes Docker support. To test with Docker:

```bash
# Build and run
docker compose up --build

# View logs
docker compose logs -f

# Rebuild after changes
docker compose down
docker compose up --build
```

## Questions?

Feel free to open an issue for:
- Questions about the codebase
- Feature requests
- Bug reports
- Documentation improvements

## License

By contributing, you agree that your contributions will be licensed under the GPL v3 License.
