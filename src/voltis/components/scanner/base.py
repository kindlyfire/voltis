from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from typing import Literal, Sequence

import anyio
import structlog
from sqlalchemy import delete, select
from sqlalchemy.dialects.postgresql import insert
from sqlalchemy.ext.asyncio import AsyncSession

from voltis.db.models import Content, ContentType, DataSource
from voltis.services.resource_broker import ResourceBroker

logger = structlog.stdlib.get_logger()


@dataclass(slots=True)
class FsItem:
    type: Literal["file", "directory"]
    path: anyio.Path
    children: list["FsItem"] | None


@dataclass(slots=True)
class ContentItem:
    uri_part: str
    title: str
    type: ContentType
    order_parts: list[int | float] | None = None
    """Will be compared in order to sort items within their parent."""
    children: list["ContentItem"] = field(default_factory=list)

    # Internal fields
    _order: int | None = None
    """The computed order based on the order_parts of all children of an
    item."""
    _content_inst: Content | None = None
    """The matching Content instance. Filled in in the matching step."""
    _content_new: bool = False
    """Whether this ContentItem represents a new Content to be inserted."""


class ScannerBase(ABC):
    def __init__(self, rb: ResourceBroker, ds: DataSource):
        self.rb = rb
        self.ds = ds

    @abstractmethod
    async def scan_items(self, items: list[FsItem]) -> list[ContentItem]:
        """
        Scan the given item and its children, returning a list of ContentItem
        instances.
        """

    @abstractmethod
    async def scan_item(self, item: ContentItem) -> None:
        """
        Analyze the ContentItem file to fill in metadata.
        """

    async def scan(self):
        logger.info("Starting scan", path=self.ds.path)
        item = await self._find_items()
        logger.info("Found items", item=item)
        await self.scan_items([item])

    async def _find_items(self) -> FsItem:
        """
        Walk all folders in self.ds.path recursively up to depth 5, returning a
        tree structure.

        Returns:
            FsItem: The root folder with all its children.
        """

        async def _inner(path: anyio.Path, depth: int) -> FsItem | None:
            if depth > 5:
                return None

            children: list[FsItem] = []

            async for item in path.iterdir():
                if await item.is_dir():
                    child_item = await _inner(item, depth + 1)
                    if child_item:
                        children.append(child_item)
                elif await item.is_file():
                    children.append(FsItem(type="file", path=item, children=None))

            return FsItem(type="directory", path=path, children=children if children else None)

        root_path = anyio.Path(self.ds.path)
        item = await _inner(root_path, depth=1)
        assert item is not None
        return item

    async def match_items(self, session: AsyncSession, items: list[ContentItem]) -> list[Content]:
        """
        Match ContentItem instances to existing Content rows in the database,
        filling in the _content_inst and _content_new fields.

        Returns:
            list[Content]: Content instances to be deleted.
        """
        contents_res = await session.scalars(
            select(Content).where(Content.datasource_id == self.ds.id)
        )
        contents = contents_res.all()
        return await self._match_items(items, contents)

    async def _match_items(
        self, items: list[ContentItem], contents: Sequence[Content]
    ) -> list[Content]:
        """See match_items."""
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
        parent_id = parent._content_inst.id if parent and parent._content_inst else None

        # We start with a full list and remove items as we match them.
        to_delete: list[Content] = [c for c in contents_map.values() if c.parent_id == parent_id]

        for item in parent_children:
            content_inst = contents_map.get((parent_id, item.uri_part))
            if content_inst:
                item._content_inst = content_inst
                item._content_new = False
                to_delete.remove(content_inst)
            else:
                item._content_inst = Content(
                    id=Content.make_id(),
                    uri_part=item.uri_part,
                    title=item.title,
                    type=item.type,
                    datasource_id=self.ds.id,
                    parent_id=parent_id,
                )
                item._content_new = True

            if item.children:
                child_deletes = await self._match_items_rec(contents_map, item, item.children)
                to_delete.extend(child_deletes)

        return to_delete

    async def save(self, items: list[ContentItem], to_delete: list[Content]) -> None:
        """
        Save the given ContentItem instances to the database. All top-level
        items are considered "complete", so data for them will be inserted,
        updated and deleted as needed to make it match.
        """
        all_items = self._flatten_items(items)

        async with self.rb.get_asession() as session:
            # Bulk upsert
            if all_items:
                objs: list[dict] = []
                for item in all_items:
                    assert item._content_inst is not None
                    objs.append(
                        {
                            "id": item._content_inst.id,
                            "uri_part": item.uri_part,
                            "title": item.title,
                            "type": item.type,
                            "datasource_id": self.ds.id,
                            "parent_id": item._content_inst.parent_id,
                            "order": item._order,
                            "order_parts": item.order_parts,
                        }
                    )

                stmt = insert(Content).values(objs)
                stmt = stmt.on_conflict_do_update(
                    index_elements=["id"],
                    set_={
                        "title": stmt.excluded.title,
                        "type": stmt.excluded.type,
                        "parent_id": stmt.excluded.parent_id,
                        "order": stmt.excluded.order,
                        "order_parts": stmt.excluded.order_parts,
                    },
                )
                await session.execute(stmt)

            # Bulk delete items
            if to_delete:
                delete_ids = [c.id for c in to_delete]
                await session.execute(delete(Content).where(Content.id.in_(delete_ids)))

            await session.commit()

    def _flatten_items(self, items: list[ContentItem]) -> list[ContentItem]:
        """Recursively flatten the tree of ContentItems into a single list."""
        result = []
        for item in items:
            result.append(item)
            result.extend(self._flatten_items(item.children))
        return result
