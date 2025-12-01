import mimetypes
import zipfile
from pathlib import Path

from fastapi import APIRouter, HTTPException
from fastapi.responses import Response

from voltis.db.models import Content
from voltis.routes._providers import RbProvider, UserProvider

router = APIRouter()

ARCHIVE_EXTENSIONS = {".cbz", ".zip"}


def _find_archive_and_inner_path(path: Path) -> tuple[Path, str] | None:
    """
    Walk up the path to find an archive file.
    Returns (archive_path, inner_path) if found, None otherwise.
    """
    parts = path.parts
    for i in range(len(parts) - 1, 0, -1):
        candidate = Path(*parts[:i])
        if candidate.is_file() and candidate.suffix.lower() in ARCHIVE_EXTENSIONS:
            inner_path = "/".join(parts[i:])
            return candidate, inner_path
    return None


def _read_from_archive(archive_path: Path, inner_path: str) -> bytes:
    """Read a file from inside a zip-based archive."""
    with zipfile.ZipFile(archive_path, "r") as zf:
        try:
            return zf.read(inner_path)
        except KeyError:
            raise HTTPException(
                status_code=404, detail=f"File not found in archive: {inner_path}"
            )


def _get_file_content(uri: str) -> tuple[bytes, str]:
    """
    Get file content from a URI, handling archives transparently.
    Returns (content_bytes, media_type).
    """
    path = Path.from_uri(uri)

    # Check if the full path exists as a regular file
    if path.is_file():
        content = path.read_bytes()
        media_type = mimetypes.guess_type(path.name)[0] or "application/octet-stream"
        return content, media_type

    # Try to find an archive in the path
    result = _find_archive_and_inner_path(path)
    if result is None:
        raise HTTPException(status_code=404, detail="File not found")

    archive_path, inner_path = result
    content = _read_from_archive(archive_path, inner_path)
    media_type = mimetypes.guess_type(inner_path)[0] or "application/octet-stream"
    return content, media_type


@router.get("/cover/{content_id}")
async def get_cover(
    rb: RbProvider,
    _user: UserProvider,
    content_id: str,
) -> Response:
    async with rb.get_asession() as session:
        content = await session.get(Content, content_id)
        if not content:
            raise HTTPException(status_code=404, detail="Content not found")

        if not content.cover_uri:
            raise HTTPException(status_code=404, detail="Content has no cover")

        data, media_type = _get_file_content(content.cover_uri)
        return Response(content=data, media_type=media_type)