import io
import zipfile
from collections.abc import AsyncIterator
from pathlib import Path

import anyio
import anyio.from_thread
import anyio.to_thread
from anyio.streams.memory import MemoryObjectSendStream
from fastapi import APIRouter, HTTPException
from fastapi.responses import FileResponse, Response
from fastapi.responses import StreamingResponse as StreamingResponse
from pydantic import BaseModel
from sqlalchemy import func, select
from sqlalchemy.orm import load_only

from voltis.components.epub import list_chapters, read_chapter
from voltis.db.models import Content
from voltis.routes._providers import RbProvider, UserProvider
from voltis.utils.cover_cache import read_content_cover, read_content_file
from voltis.utils.misc import notnone

router = APIRouter()


@router.get("/cover/{content_id}")
async def get_cover(
    rb: RbProvider,
    _user: UserProvider,
    content_id: str,
    v: str | None = None,
) -> Response:
    async with rb.get_asession() as session:
        content = await session.get(Content, content_id)
        if not content:
            raise HTTPException(status_code=404, detail="Content not found")

        if not content.cover_uri:
            raise HTTPException(status_code=404, detail="Content has no cover")

        data, media_type = await anyio.to_thread.run_sync(read_content_cover, content)

        headers = {}
        if v:
            headers["Cache-Control"] = "public, max-age=31536000, immutable"
        return Response(content=data, media_type=media_type, headers=headers)


@router.get("/comic-page/{content_id}/{page_index}")
async def get_page(
    rb: RbProvider,
    _user: UserProvider,
    content_id: str,
    page_index: int,
    v: str | None = None,
) -> Response:
    async with rb.get_asession() as session:
        content = await session.get(Content, content_id)
        if not content:
            raise HTTPException(status_code=404, detail="Content not found")

        file_data = content.file_data
        if not content.file_uri or not file_data or "pages" not in file_data:
            raise HTTPException(status_code=404, detail="Content has no pages")

        if page_index < 0 or page_index >= len(file_data["pages"]):
            raise HTTPException(status_code=404, detail="Page index out of range")

        page_name = file_data["pages"][page_index]
        file_path = Path(content.file_uri)
        page_uri = file_path / page_name[0]

        data, media_type = await anyio.to_thread.run_sync(read_content_file, page_uri.as_posix())

        headers = {}
        if v:
            headers["Cache-Control"] = "public, max-age=31536000, immutable"
        return Response(content=data, media_type=media_type, headers=headers)


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
        if not content or not content.file_uri:
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
        if not content or not content.file_uri:
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
        if not content or not content.file_uri:
            raise HTTPException(status_code=404, detail="Content not found")

        file_path = Path(content.file_uri).resolve()
        if not file_path.suffix.lower() == ".epub":
            raise HTTPException(status_code=400, detail="Content is not an EPUB")

        final_path = (file_path / path).resolve().as_posix()
        if not final_path.startswith(file_path.as_posix() + "/"):
            raise HTTPException(status_code=400, detail="Invalid resource path")

        data, media_type = await anyio.to_thread.run_sync(read_content_file, final_path)
        return Response(content=data, media_type=media_type)


class DownloadInfoResponse(BaseModel):
    file_count: int
    total_size: int | None


@router.get("/download-info/{content_id}")
async def get_download_info(
    rb: RbProvider,
    _user: UserProvider,
    content_id: str,
) -> DownloadInfoResponse:
    async with rb.get_asession() as session:
        content = await session.get(Content, content_id)
        if not content:
            raise HTTPException(status_code=404, detail="Content not found")

        if content.type in ("comic", "book"):
            if not content.file_uri:
                raise HTTPException(status_code=404, detail="Content has no file")
            return DownloadInfoResponse(
                file_count=1,
                total_size=content.file_size,
            )

        # Series: aggregate children
        result = await session.execute(
            select(
                func.count(Content.id),
                func.sum(Content.file_size),
            ).where((Content.parent_id == content_id) & (Content.file_uri.isnot(None)))
        )
        file_count, total_size = result.one()
        if not file_count:
            raise HTTPException(status_code=404, detail="No downloadable files")

        return DownloadInfoResponse(
            file_count=file_count,
            total_size=total_size,
        )


@router.get("/download/{content_id}")
async def download(
    rb: RbProvider,
    _user: UserProvider,
    content_id: str,
) -> Response:
    async with rb.get_asession() as session:
        content = await session.get(Content, content_id)
        if not content:
            raise HTTPException(status_code=404, detail="Content not found")

        if content.type in ("comic", "book"):
            if not content.file_uri:
                raise HTTPException(status_code=404, detail="Content has no file")
            file_path = Path(content.file_uri)
            return FileResponse(
                file_path,
                filename=file_path.name,
                media_type="application/octet-stream",
            )

        # Series: stream a ZIP of all children's files
        result = await session.execute(
            select(Content)
            .options(load_only(Content.file_uri, Content.uri_part))
            .where((Content.parent_id == content_id) & (Content.file_uri.isnot(None)))
            .order_by(Content.order)
        )
        children = result.scalars().all()
        if not children:
            raise HTTPException(status_code=404, detail="No downloadable files")

        file_paths = [(Path(notnone(c.file_uri)), Path(notnone(c.file_uri)).name) for c in children]
        zip_filename = f"{content.uri_part}.zip"

        async def stream_zip() -> AsyncIterator[bytes]:
            send_stream, receive_stream = anyio.create_memory_object_stream[bytes](8)

            async def build_zip() -> None:
                async with send_stream:
                    await anyio.to_thread.run_sync(_write_zip_to_stream, file_paths, send_stream)

            async with anyio.create_task_group() as tg:
                tg.start_soon(build_zip)
                async with receive_stream:
                    async for chunk in receive_stream:
                        yield chunk

        headers = {
            "Content-Disposition": f'attachment; filename="{zip_filename}"',
        }
        return StreamingResponse(
            stream_zip(),
            media_type="application/zip",
            headers=headers,
        )


def _write_zip_to_stream(
    file_paths: list[tuple[Path, str]],
    send_stream: MemoryObjectSendStream[bytes],
) -> None:
    """Build a ZIP in a worker thread, sending chunks via the stream."""
    buf = io.BytesIO()
    sent = 0
    with zipfile.ZipFile(buf, "w", compression=zipfile.ZIP_STORED) as zf:
        for file_path, arcname in file_paths:
            zf.write(file_path, arcname)
            pos = buf.tell()
            if pos > sent:
                buf.seek(sent)
                anyio.from_thread.run(send_stream.send, buf.read(pos - sent))
                sent = pos
    # Final flush for central directory
    buf.seek(sent)
    remainder = buf.read()
    if remainder:
        anyio.from_thread.run(send_stream.send, remainder)
