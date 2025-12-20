import pathlib
import zipfile

import anyio
import anyio.to_thread
import structlog
from anyio import Path

from voltis.components.epub import EpubMetadata, read_metadata
from voltis.db.models import Content
from voltis.utils.misc import now_without_tz

from .base import LibraryFile, ScannerBase

logger = structlog.stdlib.get_logger()


class BookScanner(ScannerBase):
    """
    Scanner for book files (.epub).

    Format:

    Library Root
    ├── Author Name
    │   ├── Book Title.epub
    │   ├── Series Name 01.epub
    │   └── Series Name 02.epub
    └── Another Author
        └── Standalone Book.epub

    Series information is read from EPUB metadata (Calibre or EPUB3 format).
    Books with series metadata are grouped together; standalone books have no parent.
    """

    def check_file_eligible(self, file: LibraryFile) -> bool:
        return file.uri.endswith(".epub")

    async def scan_file(self, file: LibraryFile, content: Content | None) -> Content | None:
        path = Path(file.uri)

        # Read EPUB metadata
        if not self.no_fs:
            metadata = await anyio.to_thread.run_sync(read_metadata, pathlib.Path(file.uri))
        else:
            metadata = None

        # Determine series and order
        series: Content | None = None
        series_index: float | None = None
        if metadata and metadata.series:
            series = await self._get_or_create_series(metadata.series)
            series_index = metadata.series_index

        # Build title
        if metadata and metadata.title:
            title = metadata.title
        else:
            title = path.stem

        # TODO: Unidecode title + publish date instead?
        uri_part = path.stem

        # Create or update content
        if content is None:
            content, should_skip = self._find_reusable_content(
                uri_part, series.id if series else None
            )
            if should_skip:
                return None
            if content is None:
                content = Content(
                    id=Content.make_id(),
                    library_id=self.library.id,
                    uri_part=uri_part,
                    type="book",
                    title=title,
                    file_uri=file.uri,
                    parent_id=series.id if series else None,
                    valid=True,
                    created_at=now_without_tz(),
                    updated_at=now_without_tz(),
                )
            else:
                content.title = title
                content.file_uri = file.uri
                content.parent_id = series.id if series else None
                content.valid = True
                content.updated_at = now_without_tz()
        else:
            content.title = title
            content.parent_id = series.id if series else None
            content.updated_at = now_without_tz()

        content.file_mtime = file.mtime
        content.file_size = file.size
        content.order_parts = [series_index or 0]

        if not self.no_fs:
            await anyio.to_thread.run_sync(self._scan_book, content, metadata)

        return content

    async def _get_or_create_series(self, series_name: str) -> Content:
        """Find an existing series or create a new one based on the series name."""
        uri_part = series_name

        for series in self.series:
            if series.uri_part == uri_part and series.parent_id is None:
                return series

        # Create new series
        series = Content(
            id=Content.make_id(),
            library_id=self.library.id,
            uri_part=uri_part,
            type="book_series",
            title=series_name,
            order_parts=[],
            created_at=now_without_tz(),
            updated_at=now_without_tz(),
        )

        async with self.rb.get_asession() as session:
            session.add(series)
            await session.commit()

        self.series.append(series)
        return series

    def _find_reusable_content(
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

    def _scan_book(self, content: Content, metadata: EpubMetadata | None) -> None:
        """Scan a book file (.epub) for cover and additional metadata."""
        path = pathlib.Path(content.file_uri)

        try:
            with zipfile.ZipFile(path, "r") as zf:
                if metadata and metadata.cover_path:
                    # Verify cover exists in archive
                    try:
                        zf.getinfo(metadata.cover_path)
                        content.cover_uri = f"{content.file_uri}/{metadata.cover_path}"
                    except KeyError:
                        pass

                # Store additional metadata
                if metadata:
                    meta = content.mutate_meta()
                    if metadata.authors:
                        meta["authors"] = metadata.authors
                    if metadata.description:
                        meta["description"] = metadata.description
                    if metadata.publisher:
                        meta["publisher"] = metadata.publisher
                    if metadata.language:
                        meta["language"] = metadata.language
                    if metadata.publication_date:
                        meta["publication_date"] = metadata.publication_date

        except (zipfile.BadZipFile, OSError):
            content.valid = False
