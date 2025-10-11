[positional-arguments]
run *args='':
    uv run voltis "$@"

# Format imports (I), then format code
fmt:
    uv run ruff check --select I --fix && uv run ruff format

check:
    uv run ruff format --check && uv run ruff check && uv run pyright

[positional-arguments]
test *args='':
    uv run pytest "$@"