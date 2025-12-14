import anyio
import click

from voltis.db.migrate import migrate_down, migrate_up
from voltis.services.resource_broker import ResourceBroker
from voltis.services.settings import settings


def _deploy():
    async def _inner():
        rb = ResourceBroker()
        await migrate_up(rb)

    anyio.run(_inner)


def _down():
    async def _inner():
        rb = ResourceBroker()
        await migrate_down(rb)

    anyio.run(_inner)


def _reset():
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
