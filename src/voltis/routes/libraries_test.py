import pathlib
import tempfile
import pytest


@pytest.mark.anyio
async def test_library_crud(admin_client):
    """Test create, list, update, and delete for libraries."""

    with tempfile.TemporaryDirectory() as temp_dir, tempfile.TemporaryDirectory() as temp_dir2:
        # Create a new library
        create_response = await admin_client.post(
            "libraries/new",
            json={
                "name": "test",
                "type": "comics",
                "sources": [{"path_uri": pathlib.Path(temp_dir).as_posix()}],
            },
        )
        assert create_response.status_code == 200
        library = create_response.json()
        assert library["name"] == "test"
        assert library["type"] == "comics"
        assert library["sources"] == [{"path_uri": pathlib.Path(temp_dir).as_posix()}]
        library_id = library["id"]

        # List libraries
        list_response = await admin_client.get("libraries")
        assert list_response.status_code == 200
        libraries = list_response.json()
        assert len(libraries) == 1
        assert libraries[0]["id"] == library_id

        # Update the library
        update_response = await admin_client.post(
            f"libraries/{library_id}",
            json={
                "name": "test2",
                "type": "books",
                "sources": [{"path_uri": pathlib.Path(temp_dir2).as_posix()}],
            },
        )
        assert update_response.status_code == 200
        updated = update_response.json()
        assert updated["name"] == "test2"
        assert updated["type"] == "comics"  # can't change
        assert updated["sources"] == [{"path_uri": pathlib.Path(temp_dir2).as_posix()}]

        # Delete the library
        delete_response = await admin_client.delete(f"libraries/{library_id}")
        assert delete_response.status_code == 200
        assert delete_response.json()["success"] is True

        # Verify it's gone
        list_response = await admin_client.get("libraries")
        assert list_response.status_code == 200
        assert len(list_response.json()) == 0
