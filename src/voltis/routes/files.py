from pathlib import Path

import anyio
import anyio.to_thread
from fastapi import APIRouter, HTTPException
from fastapi.responses import Response
from pydantic import BaseModel

from voltis.components.epub import list_chapters, read_chapter
from voltis.db.models import Content
from voltis.routes._providers import RbProvider, UserProvider
from voltis.utils.cover_cache import read_content_cover, read_content_file

router = APIRouter()


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

        data, media_type = await anyio.to_thread.run_sync(read_content_cover, content)
        return Response(content=data, media_type=media_type)


@router.get("/comic-page/{content_id}/{page_index}")
async def get_page(
    rb: RbProvider,
    _user: UserProvider,
    content_id: str,
    page_index: int,
) -> Response:
    async with rb.get_asession() as session:
        content = await session.get(Content, content_id)
        if not content:
            raise HTTPException(status_code=404, detail="Content not found")

        metadata = content.meta
        if not metadata or "pages" not in metadata:
            raise HTTPException(status_code=404, detail="Content has no pages")

        if page_index < 0 or page_index >= len(metadata["pages"]):
            raise HTTPException(status_code=404, detail="Page index out of range")

        page_name = metadata["pages"][page_index]
        file_path = Path(content.file_uri)
        page_uri = file_path / page_name[0]

        data, media_type = await anyio.to_thread.run_sync(read_content_file, page_uri.as_posix())
        return Response(content=data, media_type=media_type)


class ChapterResponse(BaseModel):
    id: str
    href: str
    title: str | None
    linear: bool


@router.get("/book-chapters/{content_id}")
async def get_book_chapters(
    rb: RbProvider,
    _user: UserProvider,
    content_id: str,
) -> list[ChapterResponse]:
    async with rb.get_asession() as session:
        content = await session.get(Content, content_id)
        if not content:
            raise HTTPException(status_code=404, detail="Content not found")

        file_path = Path(content.file_uri)
        if not file_path.suffix.lower() == ".epub":
            raise HTTPException(status_code=400, detail="Content is not an EPUB")

        try:
            chapters = await anyio.to_thread.run_sync(list_chapters, file_path)
        except (ValueError, FileNotFoundError) as e:
            raise HTTPException(status_code=500, detail=str(e))

        return [
            ChapterResponse(id=ch.id, href=ch.href, title=ch.title, linear=ch.linear)
            for ch in chapters
        ]


@router.get("/book-chapter/{content_id}")
async def get_book_chapter(
    rb: RbProvider,
    _user: UserProvider,
    content_id: str,
    href: str,
) -> Response:
    async with rb.get_asession() as session:
        content = await session.get(Content, content_id)
        if not content:
            raise HTTPException(status_code=404, detail="Content not found")

        file_path = Path(content.file_uri)
        if not file_path.suffix.lower() == ".epub":
            raise HTTPException(status_code=400, detail="Content is not an EPUB")

        try:
            chapter_content = await anyio.to_thread.run_sync(read_chapter, file_path, href)
        except FileNotFoundError:
            raise HTTPException(status_code=404, detail="Chapter not found")

        return Response(content=chapter_content, media_type="application/xhtml+xml")


@router.get("/book-resource/{content_id}")
async def get_book_resource(
    rb: RbProvider,
    _user: UserProvider,
    content_id: str,
    path: str,
) -> Response:
    """Serve a resource (image, CSS, etc.) from inside an EPUB file."""
    async with rb.get_asession() as session:
        content = await session.get(Content, content_id)
        if not content:
            raise HTTPException(status_code=404, detail="Content not found")

        file_path = Path(content.file_uri).resolve()
        if not file_path.suffix.lower() == ".epub":
            raise HTTPException(status_code=400, detail="Content is not an EPUB")

        final_path = (file_path / path).resolve().as_posix()
        if not final_path.startswith(file_path.as_posix() + "/"):
            raise HTTPException(status_code=400, detail="Invalid resource path")

        data, media_type = await anyio.to_thread.run_sync(read_content_file, final_path)
        return Response(content=data, media_type=media_type)
