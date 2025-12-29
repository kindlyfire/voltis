import datetime
from typing import Annotated, Literal

from fastapi import APIRouter, HTTPException, Query
from pydantic import BaseModel
from sqlalchemy import and_, delete, func, or_, select, update
from sqlalchemy.exc import IntegrityError

from voltis.db.models import (
    Content,
    CustomList,
    CustomListToContent,
    CustomListVisibility,
)
from voltis.routes._misc import OK_RESPONSE, OkResponse
from voltis.routes._providers import RbProvider, UserProvider
from voltis.utils.misc import Unset, UnsetType, now_without_tz

router = APIRouter()


class CustomListEntryDTO(BaseModel):
    id: str
    created_at: datetime.datetime
    updated_at: datetime.datetime
    library_id: str
    uri: str
    content_id: str | None
    notes: str | None
    order: int | None

    @classmethod
    def from_row(cls, entry: CustomListToContent, content_id: str | None) -> "CustomListEntryDTO":
        return cls(
            id=entry.id,
            created_at=entry.created_at,
            updated_at=entry.updated_at,
            library_id=entry.library_id,
            uri=entry.uri,
            content_id=content_id,
            notes=entry.notes,
            order=entry.order,
        )


class CustomListDTO(BaseModel):
    id: str
    created_at: datetime.datetime
    updated_at: datetime.datetime
    name: str
    description: str | None
    visibility: CustomListVisibility
    user_id: str
    entry_count: int | None

    @classmethod
    def from_model(cls, model: CustomList, entry_count: int | None = None) -> "CustomListDTO":
        return cls(
            id=model.id,
            created_at=model.created_at,
            updated_at=model.updated_at,
            name=model.name,
            description=model.description,
            visibility=model.visibility,
            user_id=model.user_id,
            entry_count=entry_count,
        )


class CustomListDetailDTO(BaseModel):
    id: str
    created_at: datetime.datetime
    updated_at: datetime.datetime
    name: str
    description: str | None
    visibility: CustomListVisibility
    user_id: str
    entry_count: int | None
    entries: list[CustomListEntryDTO]

    @classmethod
    def from_model(
        cls, model: CustomList, entries: list[CustomListEntryDTO], entry_count: int | None = None
    ) -> "CustomListDetailDTO":
        return cls(
            id=model.id,
            created_at=model.created_at,
            updated_at=model.updated_at,
            name=model.name,
            description=model.description,
            visibility=model.visibility,
            user_id=model.user_id,
            entry_count=entry_count if entry_count is not None else len(entries),
            entries=entries,
        )


class CustomListUpsertRequest(BaseModel):
    name: str
    description: str | None = None
    visibility: CustomListVisibility


class CustomListEntryCreateRequest(BaseModel):
    content_id: str
    notes: str | None = None


class CustomListEntryUpdateRequest(BaseModel):
    notes: str | None | UnsetType = Unset
    order: int | None | UnsetType = Unset


class ReorderEntriesRequest(BaseModel):
    ctc_ids: list[str]


async def _get_list_for_user(
    session, list_id: str, user, require_owner: bool = False
) -> CustomList:
    custom_list = await session.get(CustomList, list_id)
    if not custom_list:
        raise HTTPException(status_code=404, detail="List not found")

    if custom_list.user_id != user.id:
        if require_owner:
            raise HTTPException(status_code=403, detail="Not allowed")
        if custom_list.visibility == "private":
            raise HTTPException(status_code=404, detail="List not found")

    return custom_list


@router.get("")
async def list_custom_lists(
    rb: RbProvider,
    user: UserProvider,
    user_filter: Annotated[Literal["all", "me", "others"], Query()] = "all",
) -> list[CustomListDTO]:
    async with rb.get_asession() as session:
        entry_count_subq = (
            select(func.count(CustomListToContent.id).label("entry_count"))
            .where(CustomListToContent.custom_list_id == CustomList.id)
            .lateral()
        )

        owner_clause = CustomList.user_id == user.id
        others_clause = and_(CustomList.user_id != user.id, CustomList.visibility != "private")

        query = select(CustomList, entry_count_subq.c.entry_count)
        if user_filter == "me":
            query = query.where(owner_clause)
        elif user_filter == "others":
            query = query.where(others_clause)
        else:
            query = query.where(or_(owner_clause, others_clause))

        query = query.order_by(CustomList.created_at.desc())
        result = await session.execute(query)
        return [CustomListDTO.from_model(row[0], entry_count=row[1]) for row in result.all()]


@router.get("/{list_id}")
async def get_custom_list(
    rb: RbProvider,
    user: UserProvider,
    list_id: str,
) -> CustomListDetailDTO:
    async with rb.get_asession() as session:
        custom_list = await _get_list_for_user(session, list_id, user)

        entry_count = await session.scalar(
            select(func.count(CustomListToContent.id)).where(
                CustomListToContent.custom_list_id == list_id
            )
        )

        entry_rows = await session.execute(
            select(CustomListToContent, Content.id)
            .join(
                Content,
                and_(
                    Content.library_id == CustomListToContent.library_id,
                    Content.uri == CustomListToContent.uri,
                ),
                isouter=True,
            )
            .where(CustomListToContent.custom_list_id == list_id)
            .order_by(
                CustomListToContent.order.is_(None),
                CustomListToContent.order,
                CustomListToContent.created_at,
            )
        )

        entries = [
            CustomListEntryDTO.from_row(entry=row[0], content_id=row[1]) for row in entry_rows.all()
        ]

        return CustomListDetailDTO.from_model(custom_list, entries=entries, entry_count=entry_count)


@router.post("")
async def create_custom_list(
    rb: RbProvider,
    user: UserProvider,
    body: CustomListUpsertRequest,
) -> CustomListDTO:
    name = body.name.strip()
    if not name:
        raise HTTPException(status_code=400, detail="Name cannot be empty")

    async with rb.get_asession() as session:
        custom_list = CustomList(
            id=CustomList.make_id(),
            name=name,
            description=body.description,
            visibility=body.visibility,
            user_id=user.id,
            created_at=now_without_tz(),
            updated_at=now_without_tz(),
        )
        session.add(custom_list)
        await session.commit()
        await session.refresh(custom_list)
        return CustomListDTO.from_model(custom_list, entry_count=0)


@router.post("/{list_id}")
async def update_custom_list(
    rb: RbProvider,
    user: UserProvider,
    list_id: str,
    body: CustomListUpsertRequest,
) -> CustomListDTO:
    name = body.name.strip()
    if not name:
        raise HTTPException(status_code=400, detail="Name cannot be empty")

    async with rb.get_asession() as session:
        custom_list = await _get_list_for_user(session, list_id, user, require_owner=True)
        custom_list.name = name
        custom_list.description = body.description
        custom_list.visibility = body.visibility
        custom_list.updated_at = now_without_tz()

        await session.commit()
        await session.refresh(custom_list)
        entry_count = await session.scalar(
            select(func.count()).where(CustomListToContent.custom_list_id == list_id)
        )
        return CustomListDTO.from_model(custom_list, entry_count=entry_count)


@router.delete("/{list_id}")
async def delete_custom_list(
    rb: RbProvider,
    user: UserProvider,
    list_id: str,
) -> OkResponse:
    async with rb.get_asession() as session:
        custom_list = await _get_list_for_user(session, list_id, user, require_owner=True)
        await session.execute(delete(CustomList).where(CustomList.id == custom_list.id))
        await session.commit()
        return OK_RESPONSE


@router.post("/{list_id}/entries")
async def create_custom_list_entry(
    rb: RbProvider,
    user: UserProvider,
    list_id: str,
    body: CustomListEntryCreateRequest,
) -> CustomListEntryDTO:
    async with rb.get_asession() as session:
        custom_list = await _get_list_for_user(session, list_id, user, require_owner=True)

        content = await session.get(Content, body.content_id)
        if not content:
            raise HTTPException(status_code=404, detail="Content not found")

        max_order = await session.scalar(
            select(func.max(CustomListToContent.order)).where(
                CustomListToContent.custom_list_id == custom_list.id
            )
        )
        order_value = (max_order or 0) + 1

        entry = CustomListToContent(
            id=CustomListToContent.make_id(),
            custom_list_id=custom_list.id,
            library_id=content.library_id,
            uri=content.uri,
            notes=body.notes,
            order=order_value,
            created_at=now_without_tz(),
            updated_at=now_without_tz(),
        )
        session.add(entry)

        custom_list.updated_at = now_without_tz()

        try:
            await session.commit()
        except IntegrityError:
            raise HTTPException(status_code=400, detail="Content already in list")

        await session.refresh(entry)
        return CustomListEntryDTO.from_row(entry=entry, content_id=content.id)


@router.post("/{list_id}/entries/reorder")
async def reorder_custom_list_entries(
    rb: RbProvider,
    user: UserProvider,
    list_id: str,
    body: ReorderEntriesRequest,
) -> OkResponse:
    if not body.ctc_ids:
        raise HTTPException(status_code=400, detail="ctc_ids are required")

    async with rb.get_asession() as session:
        custom_list = await _get_list_for_user(session, list_id, user, require_owner=True)

        entries = await session.execute(
            select(func.count(CustomListToContent.id)).where(
                CustomListToContent.custom_list_id == custom_list.id,
                CustomListToContent.id.in_(body.ctc_ids),
            )
        )
        if entries.all()[0][0] != len(body.ctc_ids):
            raise HTTPException(status_code=400, detail="Some entries do not belong to the list")

        await session.execute(
            update(CustomListToContent),
            [
                {
                    "id": ctc_id,
                    "order": order,
                }
                for order, ctc_id in enumerate(body.ctc_ids)
            ],
        )
        custom_list.updated_at = now_without_tz()
        await session.commit()

        return OK_RESPONSE


@router.post("/{list_id}/entries/{entry_id}")
async def update_custom_list_entry(
    rb: RbProvider,
    user: UserProvider,
    list_id: str,
    entry_id: str,
    body: CustomListEntryUpdateRequest,
) -> CustomListEntryDTO:
    async with rb.get_asession() as session:
        custom_list = await _get_list_for_user(session, list_id, user, require_owner=True)
        entry = await session.get(CustomListToContent, entry_id)
        if not entry or entry.custom_list_id != custom_list.id:
            raise HTTPException(status_code=404, detail="Entry not found")

        if body.notes is not Unset:
            entry.notes = body.notes
        if body.order is not Unset:
            entry.order = body.order
        entry.updated_at = now_without_tz()
        custom_list.updated_at = now_without_tz()

        await session.commit()
        await session.refresh(entry)

        content_id = await session.scalar(
            select(Content.id).where(
                Content.library_id == entry.library_id, Content.uri == entry.uri
            )
        )
        return CustomListEntryDTO.from_row(entry=entry, content_id=content_id)


@router.delete("/{list_id}/entries/{entry_id}")
async def delete_custom_list_entry(
    rb: RbProvider,
    user: UserProvider,
    list_id: str,
    entry_id: str,
) -> OkResponse:
    async with rb.get_asession() as session:
        custom_list = await _get_list_for_user(session, list_id, user, require_owner=True)
        entry = await session.get(CustomListToContent, entry_id)
        if not entry or entry.custom_list_id != custom_list.id:
            raise HTTPException(status_code=404, detail="Entry not found")

        await session.delete(entry)
        custom_list.updated_at = now_without_tz()
        await session.commit()
        return OK_RESPONSE
