from sqlalchemy import text
from sqlalchemy.ext.asyncio import AsyncSession


async def refresh_search_index(session: AsyncSession):
    await session.execute(text("REFRESH MATERIALIZED VIEW CONCURRENTLY content_metadata_merged"))
