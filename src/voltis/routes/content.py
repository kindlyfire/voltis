import datetime
from typing import Annotated, Any

from fastapi import APIRouter, HTTPException, Query
from pydantic import BaseModel
from sqlalchemy import select

from voltis.db.models import Content, ContentType
from voltis.routes._providers import RbProvider, UserProvider

router = APIRouter()


class ContentDTO(BaseModel):
    id: str
    created_at: datetime.datetime
    updated_at: datetime.datetime
    uri_part: str
    title: str
    valid: bool
    file_uri: str
    file_mtime: datetime.datetime | None
    file_size: int | None
    cover_uri: str | None
    type: ContentType
    order: int | None
    order_parts: list[float]
    metadata_: dict[str, Any] | None
    parent_id: str | None
    library_id: str

    @classmethod
    def from_model(cls, model: Content) -> "ContentDTO":
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
            metadata_=model.metadata_,
            parent_id=model.parent_id,
            library_id=model.library_id,
        )


@router.get("/{content_id}")
async def get_content(
    rb: RbProvider,
    _user: UserProvider,
    content_id: str,
) -> ContentDTO:
    async with rb.get_asession() as session:
        content = await session.get(Content, content_id)
        if content is None:
            raise HTTPException(status_code=404, detail="Content not found")
        return ContentDTO.from_model(content)


@router.get("")
async def list_content(
    rb: RbProvider,
    _user: UserProvider,
    parent_id: Annotated[str | None, Query()] = None,
    library_id: Annotated[str | None, Query()] = None,
    type: Annotated[list[ContentType] | None, Query()] = None,
    valid: Annotated[bool, Query()] = True,
) -> list[ContentDTO]:
    async with rb.get_asession() as session:
        query = select(Content).where(Content.valid == valid)

        if parent_id is not None:
            if parent_id == "null":
                query = query.where(Content.parent_id.is_(None))
            else:
                query = query.where(Content.parent_id == parent_id)
        if library_id is not None:
            query = query.where(Content.library_id == library_id)
        if type:
            query = query.where(Content.type.in_(type))

        result = await session.scalars(query)
        return [ContentDTO.from_model(c) for c in result.all()]
