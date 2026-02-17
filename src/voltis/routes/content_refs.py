from typing import Annotated

from fastapi import APIRouter, HTTPException, Query
from pydantic import BaseModel
from sqlalchemy import delete, func, select, update

from voltis.db.models import Content, UserToContent
from voltis.routes._providers import RbProvider, UserProvider
from voltis.routes.content import UserToContentDTO
from voltis.utils.misc import PaginatedResponse

router = APIRouter()


class BrokenUserToContentDTO(UserToContentDTO):
    id: str
    uri: str
    library_id: str | None

    @classmethod
    def from_model(cls, model: UserToContent) -> "BrokenUserToContentDTO":
        return cls(
            id=model.id,
            uri=model.uri,
            library_id=model.library_id,
            starred=model.starred,
            status=model.status,
            status_updated_at=model.status_updated_at,
            notes=model.notes,
            rating=model.rating,
            progress=model.progress,
            progress_updated_at=model.progress_updated_at,
        )


class BrokenRefsSummaryItem(BaseModel):
    library_id: str | None
    count: int


class BrokenRefsFixRequest(BaseModel):
    delete: list[str] = []
    update: dict[str, str] = {}


class LibraryUrisResponse(BaseModel):
    content_uris: list[str]
    user_uris: list[str]


@router.get("/refs/{library_id}")
async def list_library_uris(
    rb: RbProvider,
    user: UserProvider,
    library_id: str,
) -> LibraryUrisResponse:
    async with rb.get_asession() as session:
        content_uris = await session.scalars(
            select(Content.uri).where(Content.library_id == library_id)
        )
        user_uris = await session.scalars(
            select(UserToContent.uri).where(
                UserToContent.user_id == user.id,
                UserToContent.library_id == library_id,
            )
        )
        return LibraryUrisResponse(
            content_uris=list(content_uris.all()),
            user_uris=list(user_uris.all()),
        )


@router.get("/broken-refs")
async def broken_refs_summary(
    rb: RbProvider,
    user: UserProvider,
) -> list[BrokenRefsSummaryItem]:
    async with rb.get_asession() as session:
        result = await session.execute(
            select(UserToContent.library_id, func.count())
            .outerjoin(
                Content,
                (Content.uri == UserToContent.uri)
                & (Content.library_id == UserToContent.library_id),
            )
            .where(UserToContent.user_id == user.id, Content.id.is_(None))
            .group_by(UserToContent.library_id)
        )
        return [BrokenRefsSummaryItem(library_id=row[0], count=row[1]) for row in result.all()]


@router.get("/broken-refs/{library_id}")
async def list_broken_refs(
    rb: RbProvider,
    user: UserProvider,
    library_id: str,
    search: Annotated[str | None, Query()] = None,
    limit: Annotated[int | None, Query(gt=0)] = None,
    offset: Annotated[int, Query(ge=0)] = 0,
) -> PaginatedResponse[BrokenUserToContentDTO]:
    async with rb.get_asession() as session:
        base = (
            select(UserToContent)
            .outerjoin(
                Content,
                (Content.uri == UserToContent.uri)
                & (Content.library_id == UserToContent.library_id),
            )
            .where(
                UserToContent.user_id == user.id,
                UserToContent.library_id == library_id,
                Content.id.is_(None),
            )
        )
        if search:
            base = base.where(UserToContent.uri.ilike(f"%{search}%"))

        total_r = await session.execute(select(func.count()).select_from(base.subquery()))
        total = total_r.scalar_one()

        data_query = base.order_by(UserToContent.uri)
        if offset:
            data_query = data_query.offset(offset)
        if limit:
            data_query = data_query.limit(limit)

        result = await session.scalars(data_query)
        return PaginatedResponse(
            data=[BrokenUserToContentDTO.from_model(row) for row in result.all()],
            total=total,
        )


@router.post("/broken-refs/{library_id}")
async def fix_broken_refs(
    rb: RbProvider,
    user: UserProvider,
    library_id: str,
    body: BrokenRefsFixRequest,
) -> None:
    async with rb.get_asession() as session:
        if body.delete:
            await session.execute(
                delete(UserToContent).where(
                    UserToContent.id.in_(body.delete),
                    UserToContent.user_id == user.id,
                    UserToContent.library_id == library_id,
                )
            )

        if body.update:
            target_uris = set(body.update.values())
            result = await session.scalars(
                select(Content.uri).where(
                    Content.uri.in_(target_uris),
                    Content.library_id == library_id,
                )
            )
            invalid = target_uris - set(result.all())
            if invalid:
                raise HTTPException(
                    status_code=400,
                    detail=f"No content with URIs {sorted(invalid)} in library '{library_id}'",
                )

            # Delete existing entries at target URIs to override the status
            await session.execute(
                delete(UserToContent).where(
                    UserToContent.user_id == user.id,
                    UserToContent.library_id == library_id,
                    UserToContent.uri.in_(target_uris),
                )
            )

        for utc_id, new_uri in body.update.items():
            await session.execute(
                update(UserToContent)
                .where(
                    UserToContent.id == utc_id,
                    UserToContent.user_id == user.id,
                    UserToContent.library_id == library_id,
                )
                .values(uri=new_uri)
            )

        await session.commit()
