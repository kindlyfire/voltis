import os

from httpx import ASGITransport, AsyncClient
import pytest
from sqlalchemy import select

from voltis.db.migrate import migrate_down, migrate_up
from voltis.db.models import User
from voltis.routes._app import create_app
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


@pytest.fixture
async def admin_client(rb):
    settings.REGISTRATION_ENABLED = True

    app = create_app(rb)
    async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test/api") as ac:
        await ac.post("auth/register", json={"username": "admin", "password": "adminpass123"})

        async with rb.get_asession() as session:
            q_user = await session.scalars(select(User).where(User.username == "admin"))
            user = q_user.first()
            assert user
            user.permissions = ["ADMIN"]
            await session.commit()

        yield ac
