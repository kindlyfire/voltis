import datetime
import mimetypes
import typing
import zipfile
from pathlib import Path

import pyvips
from fastapi import HTTPException

from voltis.db.models import Content
from voltis.services.settings import settings

COVER_MAX_WIDTH = 750
ARCHIVE_EXTENSIONS = {".cbz", ".zip", ".epub"}


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
            raise HTTPException(status_code=404, detail=f"File not found in archive: {inner_path}")


def read_content_file(uri: str) -> tuple[bytes, str]:
    """
    Get file content from a URI, handling archives transparently.
    Returns (content_bytes, media_type).
    """
    path = Path(uri)

    # Check if the full path exists as a regular file
    try:
        content = path.read_bytes()
        media_type = mimetypes.guess_type(path.name)[0] or "application/octet-stream"
        return content, media_type
    except FileNotFoundError, NotADirectoryError:
        pass

    # Try to find an archive in the path
    result = _find_archive_and_inner_path(path)
    if result is None:
        raise HTTPException(status_code=404, detail="File not found")

    archive_path, inner_path = result
    content = _read_from_archive(archive_path, inner_path)
    media_type = mimetypes.guess_type(inner_path)[0] or "application/octet-stream"
    return content, media_type


def read_content_cover(content: Content) -> tuple[bytes, str]:
    """
    Reads the cover of the image and caches it in settings.CACHE_DIR. If the
    cover is cached but its mtime is older than content.file_mtime, re-reads and
    update the cache.
    """
    if not content.cover_uri:
        raise HTTPException(status_code=404, detail="Content has no cover")

    cache_dir = Path(settings.CACHE_DIR) / "covers"
    cache_path = cache_dir / f"{content.id}.jpg"

    # Check if cache exists and is still valid
    try:
        cache_mtime = datetime.datetime.fromtimestamp(cache_path.stat().st_mtime)
        if content.file_mtime is None or cache_mtime >= content.file_mtime:
            return cache_path.read_bytes(), "image/jpeg"
    except FileNotFoundError, NotADirectoryError:
        pass

    # Read from source, resize and cache
    data, _ = read_content_file(content.cover_uri)
    data = _resize_image(data, COVER_MAX_WIDTH)

    cache_dir.mkdir(parents=True, exist_ok=True)
    cache_path.write_bytes(data)

    return data, "image/jpeg"


@typing.no_type_check
def _resize_image(data: bytes, max_width: int) -> bytes:
    """Resize image data to max_width while maintaining aspect ratio."""
    image = pyvips.Image.new_from_buffer(data, "")
    if image.width > max_width:
        scale = max_width / image.width
        image = image.resize(scale)
    return image.write_to_buffer(".jpg[Q=85]")


def delete_content_cover_cached(content_id: str) -> None:
    """Delete cached cover for content ID."""
    cache_path = Path(settings.CACHE_DIR) / "covers" / f"{content_id}.jpg"
    try:
        cache_path.unlink()
    except FileNotFoundError, NotADirectoryError:
        pass
