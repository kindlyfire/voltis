import pytest
from httpx import ASGITransport, AsyncClient

from voltis.routes._app import create_app
from voltis.services.settings import settings


@pytest.mark.anyio
async def test_user_crud(admin_client):
    """Test create, list, update, and delete for users."""

    # List users (should have the admin user from registration)
    list_response = await admin_client.get("users")
    assert list_response.status_code == 200
    users = list_response.json()
    assert len(users) == 1
    assert users[0]["username"] == "admin"

    # Create a new user
    create_response = await admin_client.post(
        "users/new",
        json={"username": "newuser", "password": "newpass123", "permissions": ["read"]},
    )
    assert create_response.status_code == 200
    user = create_response.json()
    assert user["username"] == "newuser"
    assert user["permissions"] == ["read"]
    assert "password" not in user
    assert "password_hash" not in user
    user_id = user["id"]

    # List users again
    list_response = await admin_client.get("users")
    assert list_response.status_code == 200
    assert len(list_response.json()) == 2

    # Update the user
    update_response = await admin_client.post(
        f"users/{user_id}",
        json={"username": "updateduser", "permissions": ["read", "write"]},
    )
    assert update_response.status_code == 200
    updated = update_response.json()
    assert updated["username"] == "updateduser"
    assert updated["permissions"] == ["read", "write"]

    # Delete the user
    delete_response = await admin_client.delete(f"users/{user_id}")
    assert delete_response.status_code == 200
    assert delete_response.json()["success"] is True

    # Verify it's gone
    list_response = await admin_client.get("users")
    assert list_response.status_code == 200
    assert len(list_response.json()) == 1
