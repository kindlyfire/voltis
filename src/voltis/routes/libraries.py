import datetime

import structlog
from anyio import Path
from fastapi import APIRouter, HTTPException
from pydantic import BaseModel
from sqlalchemy import func, select

from voltis.components.scanner.loader import ScannerType, get_scanner
from voltis.db.models import Content, Library
from voltis.routes._misc import OK_RESPONSE, OkResponse
from voltis.routes._providers import AdminUserProvider, RbProvider, UserProvider
from voltis.utils.misc import now_without_tz

router = APIRouter()
logger = structlog.stdlib.get_logger()


class LibrarySourceDTO(BaseModel):
    path_uri: str


class LibraryDTO(BaseModel):
    id: str
    created_at: datetime.datetime
    updated_at: datetime.datetime
    name: str
    type: ScannerType
    content_count: int | None = None
    root_content_count: int | None = None
    scanned_at: datetime.datetime | None
    sources: list[LibrarySourceDTO]

    @classmethod
    def from_model(
        cls, model: Library, content_count: int | None = None, root_content_count: int | None = None
    ) -> "LibraryDTO":
        return cls(
            id=model.id,
            created_at=model.created_at,
            updated_at=model.updated_at,
            name=model.name,
            type=model.type,
            content_count=content_count,
            root_content_count=root_content_count,
            scanned_at=model.scanned_at,
            sources=[LibrarySourceDTO(path_uri=s.path_uri) for s in model.get_sources()],
        )


class UpsertRequest(BaseModel):
    name: str
    type: ScannerType
    sources: list[LibrarySourceDTO]


@router.get("")
async def list_libraries(
    rb: RbProvider,
    _user: UserProvider,
) -> list[LibraryDTO]:
    async with rb.get_asession() as session:
        count_subq = (
            select(
                func.count(Content.id).label("content_count"),
                func.count(Content.id)
                .filter(Content.parent_id.is_(None))
                .label("root_content_count"),
            )
            .where(Content.library_id == Library.id)
            .lateral()
        )
        result = await session.execute(
            select(Library, count_subq.c.content_count, count_subq.c.root_content_count).order_by(
                Library.name
            )
        )
        return [
            LibraryDTO.from_model(v[0], content_count=v[1], root_content_count=v[2])
            for v in result.all()
        ]


class ScanResultDTO(BaseModel):
    library_id: str
    added: int
    updated: int
    removed: int
    unchanged: int


@router.post("/scan")
async def scan_libraries(
    rb: RbProvider,
    _user: AdminUserProvider,
    id: str | None = None,
    force: bool = False,
) -> list[ScanResultDTO]:
    async with rb.get_asession() as session:
        q = select(Library)
        if id:
            library_ids = [lib_id.strip() for lib_id in id.split(",")]
            q = q.where(Library.id.in_(library_ids))
        libraries = list(await session.scalars(q))

    results = []
    for library in libraries:
        logger.info("Scanning library", library_id=library.id, library_name=library.name)
        scanner = get_scanner(library.type, library, rb)
        scan_result = await scanner.scan(force=force)
        results.append(
            ScanResultDTO(
                library_id=library.id,
                added=len(scan_result.added),
                updated=len(scan_result.updated),
                removed=len(scan_result.removed),
                unchanged=len(scan_result.unchanged),
            )
        )

    return results


@router.post("/{id_or_new}")
async def upsert_library(
    rb: RbProvider,
    _user: AdminUserProvider,
    id_or_new: str,
    body: UpsertRequest,
) -> LibraryDTO:
    for source in body.sources:
        try:
            path = Path(source.path_uri)
        except Exception as e:
            logger.warning("Invalid file URI", error=str(e))
            raise HTTPException(
                status_code=400,
                detail=f"Source path is not a valid file URI: {source.path_uri}",
            )

        if not await path.is_dir():
            raise HTTPException(
                status_code=400,
                detail=f"Source path does not exist or is not a directory: {source.path_uri}",
            )

    async with rb.get_asession() as session:
        if id_or_new == "new":
            library = Library(
                id=Library.make_id(),
                name=body.name,
                type=body.type,
                sources=[s.model_dump() for s in body.sources],
                scanned_at=None,
                created_at=now_without_tz(),
                updated_at=now_without_tz(),
            )
            session.add(library)
        else:
            library = await session.get(Library, id_or_new)
            if not library:
                raise HTTPException(status_code=404, detail="Library not found")
            library.name = body.name
            library.sources = [s.model_dump() for s in body.sources]
            library.updated_at = now_without_tz()

        await session.commit()
        await session.refresh(library)
        return LibraryDTO.from_model(library)


@router.delete("/{library_id}")
async def delete_library(
    rb: RbProvider,
    _user: AdminUserProvider,
    library_id: str,
) -> OkResponse:
    async with rb.get_asession() as session:
        library = await session.get(Library, library_id)
        if not library:
            raise HTTPException(status_code=404, detail="Library not found")
        await session.delete(library)
        await session.commit()
        return OK_RESPONSE
