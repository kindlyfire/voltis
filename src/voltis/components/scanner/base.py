from abc import ABC, abstractmethod
from dataclasses import dataclass
from typing import Literal

import anyio
import structlog

from voltis.db.models import Content, DataSource
from voltis.services.resource_broker import ResourceBroker

logger = structlog.stdlib.get_logger()


@dataclass(slots=True)
class FoundItem:
    type: Literal["file", "directory"]
    path: anyio.Path
    children: list["FoundItem"] | None


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
    async def scan_items(self, items: list[FoundItem]) -> list[Content]:
        """
        Scan the given item and its children, returning a list of Content
        instances that are *not* linked to the database. They'll be matched by
        content_id and inserted.
        """

    async def find_items(self) -> FoundItem:
        """
        Walk all folders in self.ds.path recursively up to depth 5, returning a
        tree structure.

        Returns:
            A FoundItem representing the root folder with all its children.
        """

        async def _inner(path: anyio.Path, depth: int) -> FoundItem | None:
            if depth > 5:
                return None

            children: list[FoundItem] = []

            async for item in path.iterdir():
                if await item.is_dir():
                    child_item = await _inner(item, depth + 1)
                    if child_item:
                        children.append(child_item)
                elif await item.is_file():
                    children.append(FoundItem(type="file", path=item, children=None))

            return FoundItem(type="directory", path=path, children=children if children else None)

        root_path = anyio.Path(self.ds.path)
        item = await _inner(root_path, depth=1)
        assert item is not None
        return item
