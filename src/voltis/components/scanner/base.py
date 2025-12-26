import asyncio
import datetime
from abc import ABC, abstractmethod
from dataclasses import dataclass

import anyio
import anyio.to_thread
import structlog
from anyio import CapacityLimiter, Path, create_task_group
from sqlalchemy import delete, select, update

from voltis.db.models import (
    Content,
    GroupingContentTypes,
    LeafContentTypes,
    Library,
    LibrarySource,
)
from voltis.services.resource_broker import ResourceBroker
from voltis.utils.cover_cache import delete_content_cover_cached
from voltis.utils.misc import notnone, now_without_tz
from voltis.utils.time import LogTime, log_time

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
    unchanged: list[LibraryFile]


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

        self.lock = asyncio.Lock()
        """Lock that can be used in scan_file() to protect critical sections.
        Namely when looking up/creating the parent series."""

    async def scan(
        self, dry_run: bool = False, filter_paths: list[str] | None = None, force: bool = False
    ):
        return await self.scan_direct(
            await self._get_fs_items(), dry_run=dry_run, filter_paths=filter_paths, force=force
        )

    async def scan_direct(
        self,
        fs_items: list[LibraryFile],
        dry_run: bool = False,
        filter_paths: list[str] | None = None,
        force: bool = False,
    ):
        self.fs_items = fs_items
        db_items, self.series = await asyncio.gather(self._get_db_items(), self._get_db_series())

        fs_by_uri = {item.uri: item for item in fs_items}
        db_by_uri = {item[0].uri: item for item in db_items}

        to_add: list[LibraryFile] = []
        to_update: list[LibraryFile] = []
        unchanged: list[LibraryFile] = []
        for uri, item in fs_by_uri.items():
            if filter_paths and not any(uri.startswith(fp) for fp in filter_paths):
                continue
            if uri not in db_by_uri:
                to_add.append(item)
            else:
                if item.has_changed(db_by_uri[uri][0]):
                    to_update.append(item)
                else:
                    unchanged.append(item)

        if force:
            to_update.extend(unchanged)
            unchanged = []

        self.to_remove = [item for uri, item in db_by_uri.items() if uri not in fs_by_uri]
        if filter_paths:
            self.to_remove = [
                item
                for item in self.to_remove
                if any(item[0].uri.startswith(fp) for fp in filter_paths)
            ]

        if not dry_run:
            parents_with_updates: set[str] = set()
            limiter = CapacityLimiter(5)

            async def _scan_file_wrapper(item: LibraryFile, content: Content | None):
                async with limiter:
                    content = await self.scan_file(item, content)
                    if content is not None:
                        content.file_mtime = item.mtime
                        content.file_size = item.size
                        content.file_uri = item.uri
                        if content.parent_id:
                            parents_with_updates.add(content.parent_id)
                        async with self.rb.get_asession() as session:
                            session.add(content)
                            await session.commit()

            async def _scan_series_wrapper(content: Content, items: list[Content]):
                async with limiter:
                    await self.scan_series(content, items)

            async def _delete_content_cover_cached_wrapper(content_id: str):
                async with limiter:
                    await anyio.to_thread.run_sync(delete_content_cover_cached, content_id)

            async with LogTime(logger, "calling scan_file"), create_task_group() as tg:
                for item in to_add:
                    tg.start_soon(_scan_file_wrapper, item, None)
                for item in to_update:
                    tg.start_soon(_scan_file_wrapper, item, db_by_uri[item.uri][1])

            async with self.rb.get_asession() as session:
                # Update order of all items within parents that had changes
                if parents_with_updates:
                    async with LogTime(logger, "updating content order"):
                        parent_children = (
                            await session.scalars(
                                select(Content).where(Content.parent_id.in_(parents_with_updates))
                            )
                        ).all()
                        parents = (
                            await session.scalars(
                                select(Content).where(Content.id.in_(parents_with_updates))
                            )
                        ).all()

                        # Group by parent and sort by order_parts
                        by_parent: dict[str, list[Content]] = {}
                        for row in parent_children:
                            by_parent.setdefault(notnone(row.parent_id), []).append(row)

                        # Sort each group and compute new order values
                        updates = []
                        for items in by_parent.values():
                            items.sort(key=lambda x: x.order_parts)
                            for order, content in enumerate(items):
                                updates.append({"id": content.id, "order": order})

                        if updates:
                            await session.execute(update(Content), updates)
                            await session.commit()

                        async with LogTime(logger, "scanning series"), create_task_group() as tg:
                            for p in parents:
                                tg.start_soon(_scan_series_wrapper, p, by_parent.get(p.id, []))

                        await session.commit()

                # Clean up removed content + series without children
                async with (
                    LogTime(logger, "cleaning up removed content and series without children"),
                    create_task_group() as tg,
                ):
                    await session.execute(
                        delete(Content).where(
                            Content.id.in_([item[1].id for item in self.to_remove])
                        )
                    )

                    for _, content in self.to_remove:
                        tg.start_soon(
                            _delete_content_cover_cached_wrapper,
                            content.id,
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

                session.add(self.library)
                self.library.scanned_at = now_without_tz()

                await session.commit()

        return ScannerResult(
            added=to_add,
            updated=to_update,
            removed=self.to_remove,
            unchanged=unchanged,
        )

    @log_time(logger)
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
                        uri=notnone(c.file_uri),
                        mtime=c.file_mtime,
                        size=c.file_size,
                    ),
                    c,
                )
                for c in result.all()
            ]

    @log_time(logger)
    async def _get_db_series(self) -> list[Content]:
        async with self.rb.get_asession() as session:
            cursor = await session.execute(
                select(Content).where(
                    Content.library_id == self.library.id, Content.type.in_(GroupingContentTypes)
                )
            )
            return list(cursor.scalars().all())

    @log_time(logger)
    async def _get_fs_items(self) -> list[LibraryFile]:
        sources = self.library.get_sources()
        items = await asyncio.gather(
            *[self._get_fs_items_source(source) for source in sources],
        )
        return [item for sublist in items for item in sublist if self.check_file_eligible(item)]

    @log_time(logger)
    async def _get_fs_items_source(self, source: LibrarySource):
        path = Path(source.path_uri)
        limiter = CapacityLimiter(20)
        files: list[LibraryFile] = []

        async def get_file_info(item: Path) -> None:
            async with limiter:
                if await item.is_file():
                    stat = await item.stat()
                    mtime = datetime.datetime.fromtimestamp(stat.st_mtime)
                    files.append(LibraryFile(uri=item.as_posix(), mtime=mtime, size=stat.st_size))

        async with create_task_group() as tg:
            async for item in path.glob("**/*"):
                tg.start_soon(get_file_info, item)

        return files

    def find_reusable_content(
        self, uri_part: str, parent_id: str | None
    ) -> tuple[Content | None, bool]:
        """
        Find a content instance in to_remove with the same uri_part and parent.
        If found and its file no longer exists, remove it from to_remove and
        return it. If the file still exists, log a warning.

        Returns:
            (content, should_skip): content if reusable, None otherwise.
                should_skip is True if we should skip this file entirely
                (duplicate).
        """
        # TODO: If the uri_part changes, but the file URI is the same, we should
        # reuse it too.
        for i, (lib_file, content) in enumerate(self.to_remove):
            if content.uri_part != uri_part or content.parent_id != parent_id:
                continue

            # Check if the old file still exists
            file_exists = any(item.uri == lib_file.uri for item in self.fs_items)
            if file_exists:
                logger.warning(
                    "Duplicate content detected, ignoring new file",
                    uri_part=uri_part,
                    existing_file=lib_file.uri,
                )
                return None, True

            # Reuse this content - remove from to_remove list
            self.to_remove.pop(i)
            return content, False

        return None, False

    @abstractmethod
    def check_file_eligible(self, file: LibraryFile) -> bool:
        """Check if the file is eligible for processing. For example, the comic
        scanner may accept only .cbz files. Note the file may still turn out
        invalid later."""
        pass

    @abstractmethod
    async def scan_file(self, file: LibraryFile, content: Content | None) -> Content | None:
        pass

    async def scan_series(self, content: Content, items: list[Content]) -> None:
        """Will be called on any series whose files have changed. Used to for
        example set the cover to the cover of the first issue."""
