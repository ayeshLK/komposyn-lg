# REPL experience research (historical)

> **Historical research.** This comparison informed the issue #33 information hierarchy and was reviewed on 2026-10-19. Its observations and design rationale are preserved as historical context; current implementation guidance remains in [REPL information hierarchy](../../repl-experience.md).

## Research and decisions

Official documentation reviewed on 2026-10-19:

- [SQLite CLI](https://www.sqlite.org/cli.html) describes a brief startup banner
  and dedicated commands for help, database details, and versions. Apply the
  brief introduction and explicit inspection commands here.
- [PostgreSQL psql](https://www.postgresql.org/docs/current/app-psql.html)
  provides `\conninfo` for connection details and configurable prompts and
  result formatting. Keep scope visible in the prompt; make environment details
  available separately from report results.
- [IPython magics](https://ipython.readthedocs.io/en/stable/interactive/magics.html)
  exposes configuration through `%config` and reference help through `%quickref`.
  Keep help and inspection easy to discover without printing their full contents
  at startup.

These are design inferences from the tools, rather than a requirement that
KompoSyn adopt their command syntax. Plain text and whitespace provide the
hierarchy across terminals, narrow windows, and piped interactive input.
