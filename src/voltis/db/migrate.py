import structlog
from anyio import Path
from sqlalchemy import text

from voltis.services.resource_broker import ResourceBroker

logger = structlog.stdlib.get_logger()


async def migrate_up(rb: ResourceBroker):
    """Run pending migrations."""
    applied = await _get_applied_migrations(rb)

    # Find migration files
    migrations_dir = Path(__file__).parent / "migrations"
    migration_files = sorted([p async for p in migrations_dir.glob("*.sql")])

    # Run pending migrations
    for migration_file in migration_files:
        migration_name = migration_file.name.replace(".sql", "")

        if migration_name in applied:
            logger.debug(f"Already applied: {migration_name}")
            continue

        logger.info(f"Running migration: {migration_name}")

        migration_sql = await migration_file.read_text()

        async with rb.get_asession() as session:
            await session.execute(text(migration_sql))
            await session.execute(
                text("INSERT INTO _migrations (name) VALUES (:name)"),
                {"name": migration_name},
            )
            await session.commit()

    logger.info("Done.")


async def _get_applied_migrations(rb: ResourceBroker) -> set[str]:
    async with rb.get_asession() as session:
        # Create migrations table if it doesn't exist
        await session.execute(
            text("""
                CREATE TABLE IF NOT EXISTS _migrations (
                    name TEXT PRIMARY KEY,
                    applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
                )
            """)
        )
        await session.commit()

        result = await session.execute(text("SELECT name FROM _migrations"))
        return {row[0] for row in result.fetchall()}


async def migrate_down(rb: ResourceBroker):
    async with rb.get_asession() as session:
        logger.info("Reverting migrations")

        # Drop the public schema and re-create it
        await session.execute(text("DROP SCHEMA public CASCADE;"))
        await session.execute(text("CREATE SCHEMA public;"))
        await session.commit()
