import pathlib
import re
import zipfile
from io import BytesIO

import anyio
import anyio.to_thread
import imagesize
import structlog
from anyio import Path

from voltis.db.models import Content
from voltis.utils.misc import notnone, now_without_tz

from .base import LibraryFile, ScannerBase

logger = structlog.stdlib.get_logger()

IMAGE_EXTENSIONS = {".jpg", ".jpeg", ".png", ".webp", ".gif"}
COVER_NAMES = ["cover.jpg", "cover.jpeg", "cover.png"]


class ComicScanner(ScannerBase):
    """
    Scanner for comic files (.cbz).

    Format:

    Library Root
    ├── Series Name (Starting Volume Year)
    │   ├── Series Name (Vol Year) #01.cbz
    │   ├── Series Name (Vol Year) #02.cbz
    │   └── Series Name (Vol Year) #03.cbz
    └── Series Name 2 (Starting Volume Year)
        ├── Series Name 2 #01.cbz
        └── Series Name 2 #02.cbz

    Volume and chapter numbers parsing options (leading zeros are optional):

    - Volume number: #01, v01, v.01, vol.1
    - Chapter number: c01, ch01, ch.01, chap.01

    As a last resort, the first number in the filename is considered the
    volume.
    """

    def __init__(self, library, rb, no_fs=False):
        super().__init__(library, rb, no_fs)
        self._resolved_parents: dict[Path, Content] = {}

    def check_file_eligible(self, file: LibraryFile) -> bool:
        return file.uri.lower().endswith(".cbz") or file.uri.lower().endswith(".zip")

    async def scan_file(self, file: LibraryFile, content: Content | None) -> Content | None:
        path = Path(file.uri)
        series = await self._get_or_create_series(path.parent)

        # Parse volume/chapter from filename
        filename = path.stem
        vol_num = _parse_volume_number(filename)
        ch_num: int | float | None = _parse_chapter_number(filename)
        if vol_num is None and ch_num is None:
            ch_num = _parse_fallback_chapter_number(filename)

        uri_part = f"v{vol_num or 0}_ch{ch_num or 0}"

        # Build title from vol/chapter
        parts = []
        if vol_num is not None:
            parts.append(f"Vol. {vol_num}")
        if ch_num is not None:
            parts.append(f"Ch. {ch_num}")
        title = " ".join(parts) if parts else filename

        # Create or update content
        if content is None:
            content, should_skip = self.find_reusable_content(uri_part, series.id)
            if should_skip:
                return None
            if content is None:
                content = Content(
                    id=Content.make_id(),
                    library_id=self.library.id,
                    type="comic",
                    created_at=now_without_tz(),
                )

        content.file_uri = file.uri
        content.uri_part = uri_part
        content.uri = f"{series.uri}/{uri_part}"
        content.valid = True
        content.title = title
        content.parent_id = series.id
        content.updated_at = now_without_tz()
        content.order_parts = [vol_num or 0, ch_num or 0]

        if not self.no_fs:
            await anyio.to_thread.run_sync(self._scan_comic, content)

        return content

    async def _get_or_create_series(self, series_folder: Path) -> Content:
        """Find an existing series or create a new one based on the folder."""
        async with self.lock:
            if series_folder in self._resolved_parents:
                return self._resolved_parents[series_folder]

            folder_uri = series_folder.as_posix()

            name, year = _parse_series_name(series_folder.name)
            uri_part = f"{name}_{year}" if year else name

            for series in self.series:
                if series.file_uri == folder_uri:
                    self._resolved_parents[series_folder] = series
                    return series
                if series.uri_part != uri_part or series.parent_id is not None:
                    continue

                # Same series but different folder. Check if files still exist
                # in the old folder - if so, keep the old file_uri. Otherwise,
                # update to the new folder location.
                old_folder_uri = notnone(series.file_uri)
                files_in_old_folder = any(
                    item.uri.startswith(old_folder_uri) for item in self.fs_items
                )

                if not files_in_old_folder:
                    series.file_uri = folder_uri
                    await self._update_series(series)

                self._resolved_parents[series_folder] = series
                return series

            # Create new series
            series = Content(
                id=Content.make_id(),
                library_id=self.library.id,
                uri_part=uri_part,
                uri=uri_part,
                type="comic_series",
                title=name,
                file_uri=folder_uri,
                order_parts=[],
                created_at=now_without_tz(),
                updated_at=now_without_tz(),
            )
            await self._update_series(series)
            self._resolved_parents[series_folder] = series
            return series

    async def _update_series(self, series: Content):
        if not self.no_fs:
            await self._scan_series_cover(series, Path(notnone(series.file_uri)))

        async with self.rb.get_asession() as session:
            series.updated_at = now_without_tz()
            session.add(series)
            await session.commit()

        if series not in self.series:
            self.series.append(series)

    async def _scan_series_cover(self, series: Content, folder: Path) -> None:
        """Scan a comic series folder for a cover image."""
        for cover_name in COVER_NAMES:
            cover_path = folder / cover_name
            if await cover_path.is_file():
                series.cover_uri = cover_path.as_posix()
                return

    def _scan_comic(self, content: Content) -> None:
        """Scan a comic file (.cbz) for pages and cover."""
        path = pathlib.Path(notnone(content.file_uri))

        try:
            with zipfile.ZipFile(path, "r") as zf:
                pages = _list_pages(zf)
                if not pages:
                    content.valid = False
                    return
                content.mutate_meta()["pages"] = pages
                content.cover_uri = f"{content.file_uri}/{pages[0][0]}"
        except (zipfile.BadZipFile, OSError):
            content.valid = False


def _parse_volume_number(name: str) -> int | float | None:
    """
    Parse volume number from a name.

    Supports formats: #01, v01, v.01, vol.1, v1.5
    """
    if match := re.search(r"(?:#|v\.?|vol\.)\s*(\d+(?:\.\d+)?)", name, re.IGNORECASE):
        num = match.group(1)
        return float(num) if "." in num else int(num)


def _parse_chapter_number(name: str) -> int | float | None:
    """
    Parse chapter number from a name.

    Supports formats: c01, ch01, ch.01, chap.01, ch1.5
    """
    if match := re.search(r"(?:c|ch|chap)\.?\s*(\d+(?:\.\d+)?)", name, re.IGNORECASE):
        num = match.group(1)
        return float(num) if "." in num else int(num)


def _parse_fallback_chapter_number(name: str) -> int | float | None:
    """
    As a last resort, parse the number with the most digits in the name as the chapter number.
    """
    name = _clean_series_name(name)
    matches = list(re.finditer(r"(\d+(?:\.\d+)?)", name))
    if matches:
        # Find the match with the most digits (excluding the decimal point)
        def digit_count(m: re.Match[str]):
            return len(m.group(1).replace(".", ""))

        best = max(matches, key=digit_count)
        num = best.group(1)
        return float(num) if "." in num else int(num)
    return None


def _parse_series_name(name: str) -> tuple[str, int | None]:
    """
    Given a folder name, parse the series name and starting volume year
    (if any). We clean up any tags ([] or ()) at the end of the name.

    Examples:

        "My Series (2020) (something else)" -> ("My Series", 2020)
        "My Series" -> ("My Series", None)
        "My Series (Specials) [tag 1] [tag 2]" -> ("My Series (Specials)", None)
        "My Series (202A)" -> ("My Series (202A)", None)
    """
    year = _parse_series_year(name)
    cleaned = _clean_series_name(name)
    return (cleaned, year)


def _parse_series_year(name: str) -> int | None:
    """
    Parse a year from the end of a series name, if it exists.

    Examples:

        "My Series (2020)" -> 2020
        "My Series (202A)" -> None
        "My Series (90)" -> None
        "My Series (90) (2020)" -> 2020
        "My Series (90) (202)" -> None
        "My Series" -> None
    """
    matches = list(re.finditer(r"\((\d+)\)", name))
    for match in reversed(matches):
        year = int(match.group(1))
        if 1000 <= year <= 9999:
            return year
    return None


def _clean_series_name(name: str) -> str:
    """
    Removes any tags (in matching [] or ()) from the end of a series name, until none are
    left.
    """
    while True:
        cleaned = re.sub(r"\s*[\[\(][^\[\]\(\)]*[\]\)]\s*$", "", name)
        if cleaned == name:
            break
        name = cleaned
    return name.strip()


def _list_pages(zf: zipfile.ZipFile) -> list[tuple[str, int, int]]:
    """
    List image files in a zip archive, sorted naturally by filename.
    Returns paths relative to the archive root.
    """
    pages: list[tuple[str, int, int]] = []
    for info in zf.infolist():
        if info.is_dir():
            continue
        ext = pathlib.Path(info.filename).suffix.lower()
        if ext in IMAGE_EXTENSIONS:
            width, height = imagesize.get(BytesIO(zf.read(info.filename)))
            if not isinstance(width, int) or not isinstance(height, int):
                continue
            pages.append((info.filename, width, height))

    pages.sort(key=lambda x: _natural_sort_key(x[0]))
    return pages


def _natural_sort_key(s: str) -> list[int | str]:
    """Sort key for natural sorting (e.g., page2 < page10)."""
    parts: list[int | str] = []
    for part in re.split(r"(\d+)", s):
        if part.isdigit():
            parts.append(int(part))
        else:
            parts.append(part.lower())
    return parts
