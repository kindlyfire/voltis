import os

import pytest

from voltis.db.migrate import migrate_down, migrate_up
from voltis.services.resource_broker import ResourceBroker
from voltis.services.settings import settings


@pytest.fixture
def anyio_backend():
    return "asyncio"


@pytest.fixture
async def rb() -> ResourceBroker:
    if not settings.TESTS_DB_URL:
        raise ValueError("settings.TESTS_DB_URL is not set")

    settings.DB_URL = settings.TESTS_DB_URL
    os.environ["APP_DSN"] = settings.TESTS_DB_URL

    rb = ResourceBroker()

    await migrate_down(rb)
    await migrate_up(rb)

    return rb
