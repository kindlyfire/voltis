import datetime
from typing import Annotated

from fastapi import APIRouter, HTTPException, Query
from pydantic import BaseModel
from sqlalchemy import func, select
from sqlalchemy.orm import aliased

from voltis.db.models import (
    Content,
    ContentMetadata,
    ContentType,
    ReadingProgress,
    ReadingStatus,
    UserToContent,
)
from voltis.routes._providers import RbProvider, UserProvider
from voltis.utils.misc import Unset, UnsetType, now_without_tz

router = APIRouter()


class UserToContentDTO(BaseModel):
    status: ReadingStatus | None
    notes: str | None
    rating: int | None
    progress: ReadingProgress

    @classmethod
    def from_model(cls, model: UserToContent) -> "UserToContentDTO":
        return cls(
            status=model.status,
            notes=model.notes,
            rating=model.rating,
            progress=model.progress,
        )


class UserToContentRequest(BaseModel):
    status: ReadingStatus | None | UnsetType = Unset
    notes: str | None | UnsetType = Unset
    rating: int | None | UnsetType = Unset
    progress: ReadingProgress | UnsetType = Unset


class ContentDTO(BaseModel):
    id: str
    created_at: datetime.datetime
    updated_at: datetime.datetime
    uri_part: str
    title: str
    valid: bool
    file_uri: str | None
    file_mtime: datetime.datetime | None
    file_size: int | None
    cover_uri: str | None
    type: ContentType
    order: int | None
    order_parts: list[float]
    meta: ContentMetadata
    parent_id: str | None
    library_id: str
    children_count: int | None = None
    user_data: UserToContentDTO | None = None

    @classmethod
    def from_model(
        cls,
        model: Content,
        children_count: int | None = None,
        user_to_content: UserToContent | None = None,
    ) -> "ContentDTO":
        return cls(
            id=model.id,
            created_at=model.created_at,
            updated_at=model.updated_at,
            uri_part=model.uri_part,
            title=model.title,
            valid=model.valid,
            file_uri=model.file_uri,
            file_mtime=model.file_mtime,
            file_size=model.file_size,
            cover_uri=model.cover_uri,
            type=model.type,
            order=model.order,
            order_parts=model.order_parts,
            meta=model.meta,
            parent_id=model.parent_id,
            library_id=model.library_id,
            children_count=children_count,
            user_data=UserToContentDTO.from_model(user_to_content) if user_to_content else None,
        )


@router.get("/{content_id}")
async def get_content(
    rb: RbProvider,
    user: UserProvider,
    content_id: str,
) -> ContentDTO:
    async with rb.get_asession() as session:
        query = (
            select(Content, UserToContent)
            .outerjoin(
                UserToContent,
                (UserToContent.library_id == Content.library_id)
                & (UserToContent.uri == Content.uri)
                & (UserToContent.user_id == user.id),
            )
            .where(Content.id == content_id)
        )
        result = await session.execute(query)
        row = result.one_or_none()
        if row is None:
            raise HTTPException(status_code=404, detail="Content not found")
        return ContentDTO.from_model(row[0], user_to_content=row[1])


@router.get("")
async def list_content(
    rb: RbProvider,
    user: UserProvider,
    parent_id: Annotated[str | None, Query()] = None,
    library_id: Annotated[str | None, Query()] = None,
    type: Annotated[list[ContentType] | None, Query()] = None,
    valid: Annotated[bool, Query()] = True,
) -> list[ContentDTO]:
    async with rb.get_asession() as session:
        ChildContent = aliased(Content)
        count_subq = (
            select(func.count(ChildContent.id).label("children_count"))
            .where(ChildContent.parent_id == Content.id)
            .lateral()
        )
        query = (
            select(Content, count_subq.c.children_count, UserToContent)
            .outerjoin(
                UserToContent,
                (UserToContent.library_id == Content.library_id)
                & (UserToContent.uri == Content.uri)
                & (UserToContent.user_id == user.id),
            )
            .where(Content.valid == valid)
        )

        if parent_id is not None:
            if parent_id == "null":
                query = query.where(Content.parent_id.is_(None))
            else:
                query = query.where(Content.parent_id == parent_id)
        if library_id is not None:
            query = query.where(Content.library_id == library_id)
        if type:
            query = query.where(Content.type.in_(type))

        result = await session.execute(query)
        return [
            ContentDTO.from_model(row[0], children_count=row[1], user_to_content=row[2])
            for row in result.all()
        ]


@router.post("/{content_id}/user-data")
async def update_user_data(
    rb: RbProvider,
    user: UserProvider,
    content_id: str,
    body: UserToContentRequest,
) -> UserToContentDTO:
    async with rb.get_asession() as session:
        content = await session.get(Content, content_id)
        if not content:
            raise HTTPException(status_code=404, detail="Content not found")

        result = await session.execute(
            select(UserToContent).where(
                (UserToContent.user_id == user.id)
                & (UserToContent.library_id == content.library_id)
                & (UserToContent.uri == content.uri)
            )
        )
        user_to_content = result.scalar_one_or_none()

        if user_to_content is None:
            user_to_content = UserToContent(
                id=UserToContent.make_id(),
                user_id=user.id,
                library_id=content.library_id,
                uri=content.uri,
            )
            session.add(user_to_content)

        if body.status is not Unset:
            user_to_content.status = body.status
            user_to_content.status_updated_at = now_without_tz()
        if body.notes is not Unset:
            user_to_content.notes = body.notes
        if body.rating is not Unset:
            user_to_content.rating = body.rating
        if body.progress is not Unset:
            user_to_content.progress = body.progress

        await session.commit()
        await session.refresh(user_to_content)
        return UserToContentDTO.from_model(user_to_content)
