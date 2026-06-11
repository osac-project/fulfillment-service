# New Ticket Workflow

Read and execute `.ai-workflows/bugfix/skills/unattended.md` with these
settings:

- **branch**: Stay on the current branch (already created by the orchestration
  system -- do not create a new branch).
- **lint_command**: `gofmt -s -w .`
- **iteration_cap**: Maximum 3 fix-test cycles before escalating.

All artifact paths (`.artifacts/bugfix/{issue}/`) should use `.ai-bot/`
instead. Write the PR description to `.ai-bot/pr.md`.

## Repo-Specific Test Commands

Use these exact commands during the test phase:

```bash
# Unit tests (mandatory -- always run)
ginkgo run -r internal

# Focused unit tests (use during iteration to speed up feedback)
ginkgo run -r internal --focus="<test pattern matching the fix area>"
```

Do NOT run integration tests (`ginkgo run it`). They require a kind cluster
with specific `/etc/hosts` entries and are validated separately by CI.

## Repo-Specific Build Commands

```bash
go build ./cmd/fulfillment-service
go build ./cmd/osac
```

## After Proto Changes

If your fix touches any `.proto` file:

```bash
buf lint
buf generate
```

Then verify the generated code compiles:

```bash
go build ./cmd/fulfillment-service
```

## After Mock Interface Changes

If your fix modifies an interface that has a `//go:generate mockgen` directive,
regenerate the mock:

```bash
go generate ./path/to/package/
```

## Final Validation (Before Writing PR Description)

Run these in order. All must pass:

1. `gofmt -s -w .` then `git diff --exit-code` (formatting)
2. `buf lint` (proto linting, if protos changed)
3. `ginkgo run -r internal` (full unit test suite)
4. `go build ./cmd/fulfillment-service && go build ./cmd/osac` (both binaries)

## Session Context

After completing the fix, write a session context summary to
`.ai-bot/session-context.md` covering:

- Root cause summary
- Files changed and why
- Test strategy (what was tested, what was not)
- Risks or areas that need human review
