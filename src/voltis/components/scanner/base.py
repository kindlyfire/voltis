import abc
import math
from dataclasses import dataclass

import anyio
import anyio.to_thread
import structlog
from anyio import CapacityLimiter, create_task_group

from voltis.components.scanner.fs_reader import LibraryFile, get_fs_items
from voltis.components.scanner.repository import ScannerRepository
from voltis.db.models import Content, ContentMetadataDict, Library, LibrarySource
from voltis.services.resource_broker import ResourceBroker
from voltis.utils.cover_cache import delete_content_cover_cached
from voltis.utils.time import LogTime

logger = structlog.stdlib.get_logger()

_SERIES_INHERITED_FIELDS = [
    "authors",
    "publisher",
    "language",
    "genre",
    "age_rating",
    "manga",
    "imprint",
    "description",
    "publication_date",
]


@dataclass(slots=True)
class ScannerResult:
    added: list[LibraryFile]
    updated: list[LibraryFile]
    removed: list[LibraryFile]
    unchanged: list[LibraryFile]


@dataclass(slots=True)
class ScannerEventProgress:
    total: int
    processed: int


class Scanner(abc.ABC):
    def __init__(
        self,
        *,
        rb: ResourceBroker,
        library: Library,
        no_fs: bool = False,
        dry_run: bool = False,
        filter_paths: list[str] | None = None,
        force: bool = False,
        events: bool = True,
    ):
        self.rb = rb
        self.library = library
        self.r = ScannerRepository(rb, library.id)
        self.limiter = CapacityLimiter(10)
        self.no_fs = no_fs
        self.dry_run = dry_run
        self.filter_paths = filter_paths
        self.force = force
        self.events = events
        self.events_send, self.events_recv = anyio.create_memory_object_stream[
            ScannerEventProgress
        ](math.inf)
        self._progress_total = 0
        self._progress_processed = 0

    async def scan(self):
        sources = (
            self.library.get_sources()
            if not self.filter_paths
            else [LibrarySource(path_uri=f) for f in self.filter_paths]
        )
        return await self.scan_direct(
            await get_fs_items(sources, self.file_eligible),
        )

    async def scan_direct(
        self,
        files: list[LibraryFile],
    ):
        await self.r.load()

        to_add, to_update, unchanged, to_remove = self._match_files(files)
        if self.force:
            to_update.extend(unchanged)
            unchanged = []

        # Move removed items to the deleted list
        for item in to_remove:
            content = next((c for c in self.r.content if c.file_uri == item.path), None)
            if content:
                self.r.content.remove(content)
                self.r.content_d.append(content)

        # Main scan loop
        self._progress_total = len(to_add) + len(to_update)
        parents_with_updates: set[str] = set()
        async with LogTime(logger, "calling scan_file"), create_task_group() as tg:
            for item in to_add:
                tg.start_soon(self._scan_file_wrapper, item, None, parents_with_updates)
            for item in to_update:
                tg.start_soon(
                    self._scan_file_wrapper,
                    item,
                    next((c for c in self.r.content if c.file_uri == item.path)),
                    parents_with_updates,
                )

        async with LogTime(logger, "calling update_series"), create_task_group() as tg:
            for parent_id in parents_with_updates:
                parent = next((c for c in self.r.content if c.id == parent_id), None)
                if not parent:
                    continue
                items = [c for c in self.r.content if c.parent_id == parent_id]
                tg.start_soon(self._scan_series_wrapper, parent, items)

        if not self.dry_run:
            await self._commit()

        return ScannerResult(
            added=to_add,
            updated=to_update,
            removed=to_remove,
            unchanged=unchanged,
        )

    async def _commit(self):
        async with self.rb.get_asession() as session:
            await self.r.commit(session)
            await session.commit()

    def _match_files(self, files: list[LibraryFile]):
        """Takes the loaded content, and categorizes them as deleted, new, or
        existing, by comparing to the files on disk."""
        items = [c for c in self.r.content if c.type == "book" or c.type == "comic"]

        # Index by path. We turn content items into LibraryFile for easy comparison
        fs_by_path: dict[str, LibraryFile] = {item.path: item for item in files}
        db_by_path: dict[str, LibraryFile] = {}
        for item in items:
            if not item.file_uri:
                continue
            db_by_path[item.file_uri] = LibraryFile(
                path=item.file_uri,
                mtime=item.file_mtime,
                size=item.file_size,
            )

        to_add: list[LibraryFile] = []
        to_update: list[LibraryFile] = []
        unchanged: list[LibraryFile] = []
        for uri, item in fs_by_path.items():
            if self.filter_paths and not any(uri.startswith(fp) for fp in self.filter_paths):
                continue
            if uri not in db_by_path:
                to_add.append(item)
            else:
                if item.has_changed(db_by_path[uri]):
                    to_update.append(item)
                else:
                    unchanged.append(item)

        to_remove = [item for uri, item in db_by_path.items() if uri not in fs_by_path]
        if self.filter_paths:
            to_remove = [
                item
                for item in to_remove
                if any(item.path.startswith(fp) for fp in self.filter_paths)
            ]

        return to_add, to_update, unchanged, to_remove

    async def _scan_file_wrapper(
        self, file: LibraryFile, content: Content | None, parents_with_updates: set[str]
    ):
        async with self.limiter:
            content = await self.scan_file(file, content)
            if content:
                content.file_mtime = file.mtime
                content.file_size = file.size
                content.file_uri = file.path
                if content not in self.r.content:
                    self.r.content.append(content)
                if content.parent_id:
                    parents_with_updates.add(content.parent_id)
        self._progress_processed += 1
        if self.events:
            await self.events_send.send(
                ScannerEventProgress(total=self._progress_total, processed=self._progress_processed)
            )

    async def _scan_series_wrapper(self, content: Content, items: list[Content]):
        async with self.limiter:
            await self.update_series(content, items)

            # Update item order
            items.sort(key=lambda x: [(math.inf if v is None else v) for v in x.order_parts])
            for index, item in enumerate(items):
                item.order = index

    async def _delete_content_cover_cached_wrapper(self, content_id: str):
        async with self.limiter:
            await anyio.to_thread.run_sync(delete_content_cover_cached, content_id)

    def _inherit_child_metadata(self, series: Content, items: list[Content]) -> None:
        """Copy inheritable metadata fields from children to the series."""
        if not items:
            return

        inherited: ContentMetadataDict = {}
        for item in items:
            child_meta = self.r.get_metadata(uri=item.uri, provider=0).data
            for field in _SERIES_INHERITED_FIELDS:
                if field not in inherited and field in child_meta:
                    inherited[field] = child_meta[field]
            if len(inherited) == len(_SERIES_INHERITED_FIELDS):
                break

        # Derive the series title from the first child's "series" metadata
        # field, falling back to the series uri_part.
        series_title: str | None = None
        for item in items:
            child_meta = self.r.get_metadata(uri=item.uri, provider=0).data
            if "series" in child_meta:
                series_title = child_meta["series"]
                break
        if series_title is None:
            series_title = series.uri_part
        inherited["title"] = series_title

        meta_row = self.r.get_metadata(uri=series.uri, provider=0)
        meta_row.data = {**meta_row.data, **inherited}

    #
    # -- abstract methods --
    #

    @abc.abstractmethod
    async def scan_file(self, file: LibraryFile, content: Content | None) -> Content | None:
        pass

    @abc.abstractmethod
    def file_eligible(self, file: LibraryFile) -> bool:
        """Should this file go through scan_file?"""
        pass

    @abc.abstractmethod
    async def update_series(self, series: Content, items: list[Content]) -> None:
        """When at least one item in a series is updated, this function will be
        called on the series."""
        pass
