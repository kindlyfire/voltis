import typing

import click

if typing.TYPE_CHECKING:
    from ..db.models import ScannerType


@click.group()
def main(): ...


@main.command()
@click.option(
    "--host",
    default="127.0.0.1",
    show_default=True,
    help="Host address to bind the server",
)
@click.option(
    "--port",
    default=8000,
    show_default=True,
    help="Port to bind the server",
)
def run(host: str, port: int):
    """Run the app backend"""
    import logging

    import uvicorn

    from ..routes._app import create_app
    from ..services.resource_broker import ResourceBroker

    app = create_app(ResourceBroker())
    uvicorn.run(app, host=host, port=port, log_level=logging.INFO)


@main.group()
def migrate():
    """Database migration commands"""
    pass


@migrate.command()
def deploy():
    """Run migrations"""
    from .migrate import _deploy

    _deploy()


@migrate.command()
def down():
    """Drop all database tables"""
    from .migrate import _down

    _down()


@migrate.command()
def reset():
    """Reset database (drop all tables and re-run migrations)"""
    from .migrate import _reset

    _reset()


@main.group()
def devtools():
    """Development/testing tools"""


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
    """Perform a scan on a folder with a given scanner"""
    import anyio

    from ..services.resource_broker import ResourceBroker
    from .devtools.scan import _scan

    rb = ResourceBroker()
    anyio.run(_scan, rb, directory, scanner_type, library, dry_run)


@main.group()
def users():
    """User management commands"""


@users.command()
@click.argument("username")
@click.option(
    "--password",
    required=True,
    help="Password for the user ('-' to read stdin)",
)
@click.option("--admin/--no-admin", default=False, help="Grant admin permissions")
def create(username: str, password: str, admin: bool):
    """Create a new user"""
    import anyio

    from ..services.resource_broker import ResourceBroker
    from .users import _create

    rb = ResourceBroker()
    anyio.run(_create, rb, username, password, admin)


@users.command()
@click.argument("name")
@click.option("--username", help="New username")
@click.option("--password", help="New password ('-' to read stdin)")
@click.option("--admin/--no-admin", default=None, help="Grant or revoke admin permissions")
def update(name: str, username: str | None, password: str | None, admin: bool | None):
    """Update an existing user"""
    import anyio

    from ..services.resource_broker import ResourceBroker
    from .users import _update

    rb = ResourceBroker()
    anyio.run(_update, rb, name, username, password, admin)


if __name__ == "__main__":
    main()
