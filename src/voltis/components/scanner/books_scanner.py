import pathlib
import zipfile

import anyio
import anyio.to_thread
import structlog
from anyio import Path

from voltis.components.epub import EpubMetadata, read_metadata
from voltis.components.scanner.fs_reader import LibraryFile
from voltis.db.models import Content, ContentMetadataDict
from voltis.utils.misc import now_without_tz

from .base import Scanner

logger = structlog.stdlib.get_logger()


class BooksScanner(Scanner):
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

    def file_eligible(self, file: LibraryFile) -> bool:
        return file.path.lower().endswith(".epub")

    async def scan_file(self, file: LibraryFile, content: Content | None) -> Content | None:
        path = Path(file.path)

        # Read EPUB metadata
        metadata: EpubMetadata | None = None
        if not self.no_fs:
            metadata = await anyio.to_thread.run_sync(read_metadata, pathlib.Path(file.path))

        logger.info("Scanning book", file_path=file.path, metadata=metadata)

        # Determine series and order
        series: Content | None = None
        series_index: float | None = None
        if metadata and metadata.series:
            series = self.r.get_series(
                file_uri=None,
                uri_part=metadata.series,
                uri=f"book/{metadata.series}",
                type="book_series",
                title=metadata.series,
            )
            series_index = metadata.series_index

        # Build title
        title = metadata.title if metadata and metadata.title else path.stem
        uri_part = path.stem

        # Create or update content
        if content is None:
            content = self.r.match_deleted_item(uri_part, series.id if series else None)
            if content is None:
                content = Content(
                    id=Content.make_id(),
                    library_id=self.library.id,
                    type="book",
                    created_at=now_without_tz(),
                )

        content.file_uri = file.path
        content.uri_part = uri_part
        content.uri = f"{series.uri}/{uri_part}" if series else uri_part
        content.parent_id = series.id if series else None
        content.valid = True
        content.updated_at = now_without_tz()
        content.order_parts = [series_index or 0.0]

        if not self.no_fs:
            self._scan_book(content, title, metadata)
        else:
            meta_row = self.r.get_metadata(uri=content.uri)
            meta_row.set_source("file", data={"title": title})

        return content

    def _scan_book(self, content: Content, title: str, metadata: EpubMetadata | None) -> None:
        """Scan a book file (.epub) for cover and metadata."""
        assert content.file_uri
        path = pathlib.Path(content.file_uri)

        try:
            with zipfile.ZipFile(path, "r") as zf:
                if metadata and metadata.cover_path:
                    try:
                        zf.getinfo(metadata.cover_path)
                        content.cover_uri = f"{content.file_uri}/{metadata.cover_path}"
                    except KeyError:
                        pass
        except (zipfile.BadZipFile, OSError):
            content.valid = False
            return

        data: ContentMetadataDict = {"title": title}
        raw: dict = {}
        if metadata:
            if metadata.authors:
                data["authors"] = metadata.authors
            if metadata.description:
                data["description"] = metadata.description
            if metadata.publisher:
                data["publisher"] = metadata.publisher
            if metadata.language:
                data["language"] = metadata.language
            if metadata.publication_date:
                data["publication_date"] = metadata.publication_date
            if metadata.series:
                data["series"] = metadata.series
            raw = {k: v for k, v in metadata.to_object().items() if v is not None}

        meta_row = self.r.get_metadata(uri=content.uri)
        meta_row.set_source("file", data, raw=raw)

    async def update_series(self, series: Content, items: list[Content]) -> None:
        series.cover_uri = None
        series.file_mtime = None
        if items:
            series.cover_uri = items[0].cover_uri
            series.file_mtime = items[0].file_mtime
        self._inherit_child_metadata(series, items)
