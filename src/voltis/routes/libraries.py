import datetime

import anyio
import structlog
from anyio import Path
from fastapi import APIRouter, HTTPException
from pydantic import BaseModel
from sqlalchemy import select

from voltis.components.scanner.loader import ScannerType, get_scanner
from voltis.db.models import Library
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
    scanned_at: datetime.datetime | None
    sources: list[LibrarySourceDTO]

    @classmethod
    def from_model(cls, model: Library) -> "LibraryDTO":
        return cls(
            id=model.id,
            created_at=model.created_at,
            updated_at=model.updated_at,
            name=model.name,
            type=model.type,
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
        result = await session.scalars(select(Library))
        return [LibraryDTO.from_model(lib) for lib in result.all()]


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
        scan_result = await scanner.scan()
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
            path = Path.from_uri(source.path_uri)
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
            library.type = body.type
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
) -> dict:
    async with rb.get_asession() as session:
        library = await session.get(Library, library_id)
        if not library:
            raise HTTPException(status_code=404, detail="Library not found")
        await session.delete(library)
        await session.commit()
        return {"success": True}
