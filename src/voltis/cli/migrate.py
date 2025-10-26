import anyio
import click

from voltis.services.settings import settings


@click.group()
def migrate():
    """Database migration commands."""
    pass


@migrate.command()
def deploy():
    """Run migrations."""
    from ..db.migrate import migrate_up
    from ..services.resource_broker import ResourceBroker

    async def _inner():
        rb = ResourceBroker()
        await migrate_up(rb)

    anyio.run(_inner)


@migrate.command()
def down():
    """Drop all database tables."""
    from ..db.migrate import migrate_down
    from ..services.resource_broker import ResourceBroker

    async def _inner():
        rb = ResourceBroker()
        await migrate_down(rb)

    anyio.run(_inner)


@migrate.command()
def reset():
    """Reset database (drop all tables and re-run migrations)."""
    from ..db.migrate import migrate_down, migrate_up
    from ..services.resource_broker import ResourceBroker

    ans = click.prompt(
        f"Are you sure you want to reset the database? This will delete ALL DATA.\nDSN: {settings.DB_URL}\nType 'yes' to continue",
        default="no",
    )
    if ans.lower() != "yes":
        click.echo("Aborting.")
        return

    async def _inner():
        rb = ResourceBroker()
        await migrate_down(rb)
        await migrate_up(rb)

    anyio.run(_inner)
