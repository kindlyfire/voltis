from functools import cache

from sqlalchemy.ext.asyncio import (
    AsyncEngine,
    AsyncSession,
    create_async_engine,
)

from .settings import settings


class ResourceBroker:
    def __init__(self):
        pass

    @cache
    def get_aengine(self) -> AsyncEngine:
        return create_async_engine(
            settings.DB_URL,
            pool_timeout=5,
            pool_size=20,
            pool_pre_ping=True,
            pool_recycle=300,
            pool_use_lifo=True,
        )

    def get_asession(self) -> AsyncSession:
        return AsyncSession(self.get_aengine())
