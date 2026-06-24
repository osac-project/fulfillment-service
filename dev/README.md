# Development tools

Development and build tasks are automated through the `dev.py` script, which is run with `uv run
dev.py <command>`. When a new task needs to be automated (for example building, formatting,
generating code, running tests with specific options, or installing a tool), it should be added as a
new command or sub-command in the `dev/` Python package rather than as a standalone shell script or
a Makefile target.

## Running the script

The recommended way to run the `dev.py` script is through [uv](https://docs.astral.sh/uv), which
automatically manages the Python virtual environment and dependencies. For example, to run the
linter:

```shell
uv run dev.py lint
```

Since `uv run dev.py` is a common prefix, it is convenient to create a shell alias:

```shell
alias dev='uv run dev.py'
```

With this alias in place the same command becomes:

```shell
dev lint
```

## Package structure

The `dev/` package is organized as follows:

- `dev.py` - Entry point that registers all commands via Click.
- `dev/commands.py` - Helpers for running external commands, with automatic resolution of binaries
  installed in the project's `bin/` directory.
- `dev/dirs.py` - Functions that return well-known project directories (`project()`, `bin()`).
- `dev/tools.py` - Definitions of external tools (name, version, checksums) used by the `setup`
  command.
- `dev/setup.py` - The `setup` command, which downloads and verifies tool binaries from their
  GitHub release pages.
- `dev/lint.py` - The `lint` command.

To add a new command, create a new module in `dev/` (e.g. `dev/build.py`), define a Click command
in it, register it in `dev/__init__.py`, and add it to the CLI group in `dev.py`. Follow the
existing modules as examples.
