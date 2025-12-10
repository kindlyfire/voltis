import asyncio
import datetime
from abc import ABC, abstractmethod
from dataclasses import dataclass

import structlog
from anyio import Path
from sqlalchemy import delete, select

from voltis.db.models import (
    Content,
    GroupingContentTypes,
    LeafContentTypes,
    Library,
    LibrarySource,
)
from voltis.services.resource_broker import ResourceBroker

logger = structlog.stdlib.get_logger()


@dataclass(slots=True)
class LibraryFile:
    uri: str
    mtime: datetime.datetime | None = None
    size: int | None = None

    def has_changed(self, other: LibraryFile) -> bool:
        return self.mtime != other.mtime or self.size != other.size


@dataclass(slots=True)
class ScannerResult:
    added: list[LibraryFile]
    updated: list[LibraryFile]
    removed: list[tuple[LibraryFile, Content]]


class ScannerBase(ABC):
    """
    How scanning works:

    - We take all files on disk, all files in the database, and compare mtime /
      size to establish created, updated and deleted lists
    - We call `scan_file` on each new and updated file
    - `scan_file` will look up the right series, creating it if necessary, and
      return the Content instance for the file (a new one if it's a new file, or
      the updated one it was given as input otherwise)
        - It may un-delete content if it was marked for deletion and update it
          instead (when a file is moved)

    Files and series have a `uri_part` that uniquely identifies them within the
    library. `scan_file` will compute the `uri_part` and match based on that.
    Series also work in the same way.

    ## Moving or splitting series

    We can detect and handle the moving of a series from one source folder to
    another, as well as the files of a series being split across sources. The
    series itself will only be linked to one of the source folders, but the
    files should scan fine.
    """

    def __init__(self, library: Library, rb: ResourceBroker, no_fs: bool = False):
        self.library = library
        self.rb = rb

        self.no_fs = no_fs
        """Disable doing any file operations. Used in tests. We just de
        everything we can from the info available in LibraryFile."""

        self.to_remove: list[tuple[LibraryFile, Content]] = []
        """
        Kept track of class-wide as a scan() of a file that has been added may
        instead "take over" a file that was going to be removed. For example,
        when moving a file from one library source to another.
        """

        self.series: list[Content] = []
        """To be used in scan_file() to match items to a series, and create it
        otherwise."""

        self.fs_items: list[LibraryFile] = []
        """To be used in scan_file() to check if a series with the right
        uri_part but a different folder should have its file_uri updated. If
        files still exist in the old folder, it should not be updated."""

    async def scan(self, dry_run: bool = False):
        return await self.scan_direct(await self._get_fs_items(), dry_run=dry_run)

    async def scan_direct(self, fs_items: list[LibraryFile], dry_run: bool = False):
        self.fs_items = fs_items
        db_items, self.series = await asyncio.gather(self._get_db_items(), self._get_db_series())

        fs_by_uri = {item.uri: item for item in fs_items}
        db_by_uri = {item[0].uri: item for item in db_items}

        to_add = [item for uri, item in fs_by_uri.items() if uri not in db_by_uri]
        to_update = [
            item
            for uri, item in fs_by_uri.items()
            if uri in db_by_uri and item.has_changed(db_by_uri[uri][0])
        ]
        self.to_remove = [item for uri, item in db_by_uri.items() if uri not in fs_by_uri]

        if not dry_run:
            for item in to_add:
                content = await self.scan_file(item, None)
                if content is not None:
                    async with self.rb.get_asession() as session:
                        session.add(content)
                        await session.commit()

            for item in to_update:
                content = await self.scan_file(item, db_by_uri[item.uri][1])
                if content is not None:
                    async with self.rb.get_asession() as session:
                        session.add(content)
                        await session.commit()

            # Clean up removed content + series without children
            async with self.rb.get_asession() as session:
                await session.execute(
                    delete(Content).where(Content.id.in_([item[1].id for item in self.to_remove]))
                )

                parent_ids_with_children = (
                    select(Content.parent_id).where(Content.parent_id.isnot(None)).distinct()
                )
                await session.execute(
                    delete(Content).where(
                        Content.library_id == self.library.id,
                        Content.type.in_(GroupingContentTypes),
                        Content.id.notin_(parent_ids_with_children),
                    )
                )
                await session.commit()

        return ScannerResult(
            added=to_add,
            updated=to_update,
            removed=self.to_remove,
        )

    async def _get_db_items(self) -> list[tuple[LibraryFile, Content]]:
        async with self.rb.get_asession() as session:
            result = await session.scalars(
                select(Content).where(
                    Content.library_id == self.library.id, Content.type.in_(LeafContentTypes)
                )
            )
            return [
                (
                    LibraryFile(
                        uri=c.file_uri,
                        mtime=c.file_mtime,
                        size=c.file_size,
                    ),
                    c,
                )
                for c in result.all()
            ]

    async def _get_db_series(self) -> list[Content]:
        async with self.rb.get_asession() as session:
            cursor = await session.execute(
                select(Content).where(
                    Content.library_id == self.library.id, Content.type.in_(GroupingContentTypes)
                )
            )
            return list(cursor.scalars().all())

    async def _get_fs_items(self) -> list[LibraryFile]:
        sources = self.library.get_sources()
        tasks = [self._get_fs_items_source(source) for source in sources]
        items = await asyncio.gather(*tasks)
        return [item for sublist in items for item in sublist if self.check_file_eligible(item)]

    async def _get_fs_items_source(self, source: LibrarySource):
        path = Path.from_uri(source.path_uri)
        files: list[LibraryFile] = []
        async for item in path.glob("**/*"):
            if await item.is_file():
                stat = await item.stat()
                mtime = datetime.datetime.fromtimestamp(stat.st_mtime)
                files.append(LibraryFile(uri=item.as_uri(), mtime=mtime, size=stat.st_size))
        return files

    @abstractmethod
    def check_file_eligible(self, file: LibraryFile) -> bool:
        """Check if the file is eligible for processing. For example, the comic
        scanner may accept only .cbz files. Note the file may still turn out
        invalid later."""
        pass

    @abstractmethod
    async def scan_file(self, file: LibraryFile, content: Content | None) -> Content | None:
        pass
