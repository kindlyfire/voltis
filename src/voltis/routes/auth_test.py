import pytest
from httpx import ASGITransport, AsyncClient

from voltis.services.settings import settings


@pytest.fixture
async def client(rb):
    """Create an async HTTP client for testing the FastAPI app."""
    from voltis.routes._app import create_app

    app = create_app(rb)
    async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as ac:
        yield ac


@pytest.mark.anyio
async def test_register_and_login(client):
    """Test the complete registration and login flow."""
    settings.REGISTRATION_ENABLED = True

    username = "testuser"
    password = "testpass123"

    # Register a new user
    register_response = await client.post(
        "/auth/register",
        json={"username": username, "password": password},
    )
    assert register_response.status_code == 200
    assert register_response.json()["success"] is True
    assert "voltis_session" in register_response.cookies

    # Login with the same credentials
    login_response = await client.post(
        "/auth/login",
        json={"username": username, "password": password},
    )
    assert login_response.status_code == 200
    assert login_response.json()["success"] is True
    assert "voltis_session" in login_response.cookies
