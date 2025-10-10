from sqlalchemy import text
from voltis.services.resource_broker import ResourceBroker


async def migrate_up():
    pass


async def migrate_down(rb: ResourceBroker):
    async with rb.get_asession() as session:
        # Drop the public schema and re-create it
        await session.execute(text("DROP SCHEMA public CASCADE;"))
        await session.execute(text("CREATE SCHEMA public;"))
