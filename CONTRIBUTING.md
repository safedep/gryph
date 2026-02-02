# Contributing to Gryph

Thanks for your interest in contributing to Gryph!

## Getting Started

```bash
git clone https://github.com/safedep/gryph.git
cd gryph
make gryph
```

## Development Workflow

1. Fork the repository and create a branch from `main`
2. Make your changes
3. Run tests and formatting:

```bash
make test
make fmt
make lint
```

4. Submit a pull request

## Project Structure

```
core/       Domain models (events, sessions, audit, security)
config/     Viper-based configuration
storage/    SQLite + ent ORM
agent/      Agent adapters (claudecode/, cursor/, etc.)
cli/        Cobra commands
tui/        Output formatters
```

If you modify ent schemas in `storage/ent/`, run `make generate` to regenerate the ORM code.

## Guide

- See [agent adapter](/docs/agent-adapter.md) for adding support for new coding agents

## Reporting Issues

Open an issue on [GitHub](https://github.com/safedep/gryph/issues) with steps to reproduce and relevant environment details.

## License

By contributing, you agree that your contributions will be licensed under the [Apache 2.0 License](LICENSE).
