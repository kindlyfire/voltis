import typing

import click

if typing.TYPE_CHECKING:
    from ..db.models import ScannerType


@click.group()
def main(): ...


@main.command()
def run():
    """Run the app backend"""
    import logging

    import uvicorn

    from ..routes._app import create_app
    from ..services.resource_broker import ResourceBroker

    app = create_app(ResourceBroker())
    uvicorn.run(app, host="127.0.0.1", port=8000, log_level=logging.INFO)


@main.group()
def migrate():
    """Database migration commands."""
    pass


@migrate.command()
def deploy():
    """Run migrations."""
    from .migrate import _deploy

    _deploy()


@migrate.command()
def down():
    """Drop all database tables."""
    from .migrate import _down

    _down()


@migrate.command()
def reset():
    """Reset database (drop all tables and re-run migrations)."""
    from .migrate import _reset

    _reset()


@main.group()
def devtools():
    """Development/testing tools."""


@devtools.command()
@click.argument(
    "directory", type=click.Path(exists=True, file_okay=False, dir_okay=True), required=False
)
@click.option(
    "-t",
    "--type",
    "scanner_type",
    type=click.Choice(["comics", "books"]),
    help="Type of scanner to use",
)
@click.option(
    "--library",
    type=str,
    help="Library ID to use (if not provided, use in-memory library)",
)
@click.option(
    "--dry-run",
    is_flag=True,
    help="Show what would be done without saving to database",
)
def scan(
    directory: str | None,
    scanner_type: ScannerType | None,
    library: str | None,
    dry_run: bool,
):
    """Perform a scan on a folder with a given scanner."""
    import anyio

    from ..services.resource_broker import ResourceBroker
    from .devtools.scan import _scan

    rb = ResourceBroker()
    anyio.run(_scan, rb, directory, scanner_type, library, dry_run)


if __name__ == "__main__":
    main()
