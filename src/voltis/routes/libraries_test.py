import pytest
from httpx import ASGITransport, AsyncClient

from voltis.routes._app import create_app
from voltis.services.settings import settings


@pytest.fixture
async def client(rb):
    settings.REGISTRATION_ENABLED = True

    app = create_app(rb)
    async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as ac:
        await ac.post("/auth/register", json={"username": "admin", "password": "adminpass123"})
        yield ac


@pytest.mark.anyio
async def test_library_crud(client):
    """Test create, list, update, and delete for libraries."""

    # Create a new library
    create_response = await client.post(
        "/libraries/new",
        json={"type": "comics", "sources": [{"path_uri": "file:///test/path"}]},
    )
    assert create_response.status_code == 200
    library = create_response.json()
    assert library["type"] == "comics"
    assert library["sources"] == [{"path_uri": "file:///test/path"}]
    library_id = library["id"]

    # List libraries
    list_response = await client.get("/libraries")
    assert list_response.status_code == 200
    libraries = list_response.json()
    assert len(libraries) == 1
    assert libraries[0]["id"] == library_id

    # Update the library
    update_response = await client.post(
        f"/libraries/{library_id}",
        json={"type": "books", "sources": [{"path_uri": "file:///updated/path"}]},
    )
    assert update_response.status_code == 200
    updated = update_response.json()
    assert updated["type"] == "books"
    assert updated["sources"] == [{"path_uri": "file:///updated/path"}]

    # Delete the library
    delete_response = await client.delete(f"/libraries/{library_id}")
    assert delete_response.status_code == 200
    assert delete_response.json()["success"] is True

    # Verify it's gone
    list_response = await client.get("/libraries")
    assert list_response.status_code == 200
    assert len(list_response.json()) == 0
