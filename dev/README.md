# Development tools

Development and build tasks are automated through the `dev.py` script, which is run with `uv run
dev.py <command>`. When a new task needs to be automated (for example building, formatting,
generating code, running tests with specific options, or installing a tool), it should be added as a
new command or sub-command in the `dev/` Python package rather than as a standalone shell script or
a Makefile target.

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
