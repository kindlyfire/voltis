import datetime
import math
from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from typing import Sequence

import structlog
from sqlalchemy import delete, select
from sqlalchemy.dialects.postgresql import insert
from sqlalchemy.ext.asyncio import AsyncSession

from voltis.components.scanner.fs_item import FsItem, list_path_uri_items
from voltis.db.models import Content, ContentType
from voltis.utils.misc import now_without_tz

logger = structlog.stdlib.get_logger()


@dataclass(slots=True)
class ContentItem:
    uri_part: str
    title: str
    type: ContentType
    file_uri: str = ""
    cover_uri: str | None = None
    order_parts: list[int | float] = field(default_factory=list)
    """Will be compared in order to sort items within their parent."""
    children: list["ContentItem"] = field(default_factory=list)
    file_modified_at: datetime.datetime | None = None
    metadata: dict = field(default_factory=dict)

    order: int | None = None
    """Do not set in scanner impl. The computed order based on the order_parts
    of all children of an item."""
    content_inst: Content | None = None
    """Do not set in scanner impl. The matching Content instance. Filled in in
    the matching step."""
    content_new: bool = False
    """Do not set in scanner impl. Whether this ContentItem represents a new
    Content to be inserted."""

    @classmethod
    def flatten(cls, item: list[ContentItem]) -> list[ContentItem]:
        result: list[ContentItem] = []
        for it in item:
            result.append(it)
            result.extend(cls.flatten(it.children or []))
        return result


class ScannerBase(ABC):
    """Base class for all scanners. Scanners should keep no internal state."""

    @abstractmethod
    async def scan_items(self, items: list[FsItem]) -> list[ContentItem]:
        """
        Scan the given item and its children, returning a list of ContentItem
        instances.
        """
        raise NotImplementedError()

    @abstractmethod
    async def scan_item(self, item: ContentItem) -> None:
        """
        Analyze the ContentItem file to fill in metadata.
        """
        raise NotImplementedError()

    async def scan(self, path_uri: str) -> list[ContentItem]:
        logger.info("Starting scan", path=path_uri)
        item = await list_path_uri_items(path_uri)
        logger.info("Found items", item=item)
        return await self.scan_items([item])

    async def match_from_db(
        self, session: AsyncSession, datasource_id: str, items: list[ContentItem]
    ) -> list[Content]:
        """
        Match ContentItem instances to existing Content rows in the database,
        filling in the .content_inst and .content_new fields.

        Returns:
            list[Content]: Content instances to be deleted.
        """
        contents_res = await session.scalars(
            select(Content).where(Content.datasource_id == datasource_id)
        )
        return await self.match_from_instances(items, contents_res.all())

    async def match_from_instances(
        self, items: list[ContentItem], contents: Sequence[Content]
    ) -> list[Content]:
        """See match_from_db. Different in that you provide the Content
        instances."""
        contents_map = {(c.parent_id, c.uri_part): c for c in contents}
        return await self._match_items_rec(contents_map, None, items)

    async def _match_items_rec(
        self,
        contents_map: dict[tuple[str | None, str], Content],
        parent: ContentItem | None,
        parent_children: list[ContentItem],
    ) -> list[Content]:
        """The recursive part of match_items, walking through the tree of
        ContentItem instances to match them with Content instances."""
        parent_id = parent.content_inst.id if parent and parent.content_inst else None

        # We start with a full list and remove items as we match them.
        to_delete: list[Content] = [c for c in contents_map.values() if c.parent_id == parent_id]
        children: list[Content] = []

        for item in parent_children:
            content_inst = contents_map.get((parent_id, item.uri_part))
            if content_inst:
                to_delete.remove(content_inst)
            else:
                content_inst = Content(
                    id=Content.make_id(),
                    uri_part=item.uri_part,
                    valid=True,
                    type=item.type,
                    parent_id=parent_id,
                    created_at=now_without_tz(),
                    updated_at=now_without_tz(),
                )
                item.content_new = True

            children.append(content_inst)
            item.content_inst = content_inst

            content_inst.title = item.title
            content_inst.file_uri = item.file_uri
            content_inst.cover_uri = item.cover_uri
            content_inst.order_parts = item.order_parts
            content_inst.metadata_ = item.metadata
            content_inst.file_modified_at = item.file_modified_at

            if item.children:
                child_deletes = await self._match_items_rec(contents_map, item, item.children)
                to_delete.extend(child_deletes)

        # Sort children by order_parts
        max_len = max((len(c.order_parts) for c in children), default=0)
        children.sort(key=lambda x: x.order_parts + [-math.inf] * (max_len - len(x.order_parts)))
        for order, child in enumerate(children):
            child.order = order

        return to_delete

    async def save(
        self,
        session: AsyncSession,
        datasource_id: str,
        to_upsert: list[ContentItem],
        to_delete: list[Content],
    ) -> None:
        """Save the given ContentItem instances to the database."""
        all_items = ContentItem.flatten(to_upsert)

        upsert_objs: list[dict] = []
        for item in all_items:
            assert item.content_inst is not None
            if not item.content_inst.has_changes():
                continue

            item.content_inst.updated_at = now_without_tz()
            item.content_inst.datasource_id = datasource_id
            upsert_objs.append(item.content_inst.as_dict())

        if upsert_objs:
            stmt = insert(Content).values(upsert_objs)
            stmt = stmt.on_conflict_do_update(
                index_elements=["id"],
                set_={
                    "title": stmt.excluded.title,
                    "type": stmt.excluded.type,
                    "file_uri": stmt.excluded.file_uri,
                    "cover_uri": stmt.excluded.cover_uri,
                    "parent_id": stmt.excluded.parent_id,
                    "order": stmt.excluded.order,
                    "order_parts": stmt.excluded.order_parts,
                    "metadata": stmt.excluded.metadata,
                    "file_modified_at": stmt.excluded.file_modified_at,
                },
            )
            await session.execute(stmt)

        # Bulk delete items
        if to_delete:
            delete_ids = [c.id for c in to_delete]
            await session.execute(delete(Content).where(Content.id.in_(delete_ids)))

        await session.commit()
