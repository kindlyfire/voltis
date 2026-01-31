import datetime
import re
import zipfile

import anyio
import anyio.to_thread
import structlog
from anyio import Path
import vmeta

from voltis.db.models import Content, ContentMetadata
from voltis.utils.misc import notnone, now_without_tz

from .base import LibraryFile, ScannerBase

logger = structlog.stdlib.get_logger()

IMAGE_EXTENSIONS = {".jpg", ".jpeg", ".png", ".webp", ".gif"}
COVER_NAMES = ["cover.jpg", "cover.jpeg", "cover.png", "cover.webp"]


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
        meta, valid = await anyio.to_thread.run_sync(self._scan_comic, path)
        if not valid:
            return None

        fallback_name, fallback_year = _parse_series_name(path.parent.name)
        name = meta.get("series") or fallback_name
        year = meta.get("year") or fallback_year
        uri_part = f"{name}_{year}" if year else name
        series = await self.find_or_create_series(
            file_uri=path.parent.as_posix(),
            uri_part=uri_part,
            uri=f"comic/{uri_part}",
            type="comic_series",
            title=name,
        )

        # Parse volume/chapter from filename
        filename = path.stem
        vol_num = meta["volume"] if "volume" in meta else _parse_volume_number(filename)
        ch_num: str | int | float | None = (
            meta["number"] if "number" in meta else _parse_chapter_number(filename)
        )
        if isinstance(ch_num, str):
            try:
                ch_num = float(ch_num)
            except ValueError:
                ch_num = _parse_chapter_number(ch_num)
        if vol_num is None and ch_num is None:
            ch_num = _parse_fallback_chapter_number(filename)

        uri_part = f"v{vol_num or 0}_ch{ch_num or 0}"

        # Build title from vol/chapter
        parts = []
        if vol_num is not None:
            parts.append(f"Vol. {vol_num:g}")
        if ch_num is not None:
            parts.append(f"Ch. {ch_num:g}")
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
        content.order_parts = [vol_num, ch_num]
        assert "pages" in meta
        content.cover_uri = f"{content.file_uri}/{meta['pages'][0][0]}"

        m = content.mutate_meta()
        for k, v in meta.items():
            m[k] = v

        return content

    def _scan_comic(self, path: Path) -> tuple[ContentMetadata, bool]:
        """Scan a comic file (.cbz) for pages, cover, and metadata."""
        m = ContentMetadata()
        try:
            meta = vmeta.read_metadata_and_pages(path.as_posix())
            pages: list[tuple[str, int, int]] = []

            for page in meta["pages"]:
                if "error" in page:
                    raise ValueError("PageInfoError: " + page["error"])
                pages.append((page["name"], page["width"], page["height"]))

            if not pages:
                return m, False
            m["pages"] = pages
            # content.cover_uri = f"{content.file_uri}/{pages[0][0]}"
            comic_info = meta.get("metadata")
            if comic_info:
                _store_comic_metadata(m, comic_info)
            return m, True
        except (zipfile.BadZipFile, OSError):
            return m, False

    async def _scan_series_cover(self, series: Content, folder: Path) -> None:
        """Scan a comic series folder for a cover image."""
        for cover_name in COVER_NAMES:
            cover_path = folder / cover_name
            if await cover_path.is_file():
                series.cover_uri = cover_path.as_posix()
                stat = await cover_path.stat()
                series.file_mtime = datetime.datetime.fromtimestamp(stat.st_mtime)
                return

    async def scan_series(self, content, items):
        content.cover_uri = None
        content.file_mtime = None
        await self._scan_series_cover(content, Path(notnone(content.file_uri)))
        if items:
            if not content.cover_uri:
                content.cover_uri = items[0].cover_uri
            content.file_mtime = items[0].file_mtime

            # Merge metadata from ComicInfo of all items
            ignore_keys = {"pages", "series", "volume", "number", "title"}
            m = content.mutate_meta()
            for item in items:
                item_meta = item.mutate_meta()
                for k, v in item_meta.items():
                    if k not in ignore_keys and k not in m:
                        m[k] = v


def _parse_volume_number(name: str) -> float | None:
    """
    Parse volume number from a name.

    Supports formats: #01, v01, v.01, vol.1, v1.5, volume 1 (any prefix of "volume",
    with optional leading/trailing dot and spaces).
    """
    volume_pattern = r"(?:\#|(?:v|vo|vol|volu|volum|volume)\.?)\s*(?P<num>\d+(?:\.\d+)?)"
    if match := re.search(volume_pattern, name, re.IGNORECASE):
        num = match.group("num")
        return float(num)


def _parse_chapter_number(name: str) -> float | None:
    """
    Parse chapter number from a name.

    Supports formats: c01, ch01, ch.01, chap.01, ch1.5, chapter 1 (any prefix of
    "chapter" with optional trailing dot and spaces).
    """
    chapter_pattern = r"(?:c|ch|chap|chapt|chapte|chapter)\.?\s*(?P<num>\d+(?:\.\d+)?)"
    if match := re.search(chapter_pattern, name, re.IGNORECASE):
        num = match.group("num")
        return float(num)


def _parse_fallback_chapter_number(name: str) -> float | None:
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
        return float(num)
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


_COMIC_DIRECT_FIELDS = [
    "title",
    "series",
    "writer",
    "penciller",
    "inker",
    "colorist",
    "letterer",
    "cover_artist",
    "editor",
    "genre",
    "age_rating",
    "manga",
    "characters",
    "teams",
    "locations",
    "story_arc",
    "series_group",
    "format",
    "imprint",
    "web",
    "notes",
    "scan_information",
    "black_and_white",
    "community_rating",
    "review",
    "main_character_or_team",
    "alternate_series",
    "alternate_number",
    "alternate_count",
    "count",
    "number",
    "volume",
    "publisher",
]


def _store_comic_metadata(m: ContentMetadata, comic_info: vmeta.ComicInfo) -> None:
    """Extract ComicInfo metadata into ContentMetadata dict."""
    if "summary" in comic_info:
        m["description"] = comic_info["summary"]
    if "language_iso" in comic_info:
        m["language"] = comic_info["language_iso"]

    if "year" in comic_info:
        y = comic_info["year"]
        mo = comic_info.get("month")
        d = comic_info.get("day")
        if mo and d:
            m["publication_date"] = f"{y:04d}-{mo:02d}-{d:02d}"
        else:
            m["publication_date"] = str(y)

    if "writer" in comic_info:
        m["authors"] = [a.strip() for a in comic_info["writer"].split(",")]

    for field in _COMIC_DIRECT_FIELDS:
        val = comic_info.get(field)
        if val is not None and val != "" and val != "Unknown":
            m[field] = val


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
