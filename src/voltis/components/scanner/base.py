from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from typing import Literal

import anyio
import structlog
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
    content_id: str
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

    async def scan(self):
        logger.info("Starting scan", path=self.ds.path)
        item = await self.find_items()
        logger.info("Found items", item=item)
        await self.scan_items([item])

    @abstractmethod
    async def scan_items(self, items: list[FsItem]) -> list[ContentItem]:
        """
        Scan the given item and its children, returning a list of ContentItem
        instances that are *not* linked to the database. They'll be matched by
        content_id and inserted.
        """

    async def find_items(self) -> FsItem:
        """
        Walk all folders in self.ds.path recursively up to depth 5, returning a
        tree structure.

        Returns:
            A FoundItem representing the root folder with all its children.
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
            A list of Content instances to be deleted.
        """
        raise NotImplementedError()

    async def save(self, items: list[ContentItem]) -> None:
        """
        Save the given ContentItem instances to the database. All top-level
        items are considered "complete", so data for them will be inserted,
        updated and deleted as needed to make it match.
        """

        async with self.rb.get_asession() as session:
            raise NotImplementedError()
