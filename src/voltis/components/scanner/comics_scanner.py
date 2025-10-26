import re

from .base import ContentItem, FsItem, ScannerBase


class ComicScanner(ScannerBase):
    async def scan_items(self, items: list[FsItem]) -> list[ContentItem]:
        """
        We scan walk through folders. If a folder contains a .cbz file, we
        consider it a comic folder.

        A "Specials" folder may exist inside a series folder, which will create
        a sub-series.

        We create one Content for the series, and then one child Content for
        each volume/chapter.

        Format:

        Library Root
        ┖── Series Name (Starting Volume Year)
            ┠── Series Name (Vol Year) #01.cbz
            ┠── Series Name (Vol Year) #02.cbz
            ┠── Series Name (Vol Year) #03.cbz
        ┖── Series Name 2 (Starting Volume Year)
            ┠── Series Name 2 #01.cbz
            ┠── Series Name 2 #02.cbz
            ⋮
            ┖── Series Name 2 #06.cbz
        ┖── Some Other Folder
            ┖── Series Name 2 (Starting Volume Year)
                ┠── Series Name 2 #01.cbz
                ┠── Series Name 2 #02.cbz
                ⋮
                ┖── Series Name 2 #06.cbz

        Volume and chapter numbers parsing options (leading zeros are optional):

        - Volume number: #01, v01, v.01, vol.1
        - Chapter number: c01, ch01, ch.01, chap.01

        As a last resort, the first number in the filename is considered the
        volume.
        """
        contents: list[ContentItem] = []

        # Process each root item
        for item in items:
            if item.type == "directory" and item.children:
                contents.extend(await self._process_children(item))

        return contents

    async def _process_children(self, parent: FsItem) -> list[ContentItem]:
        """Process all children of a directory."""
        contents: list[ContentItem] = []
        for child in parent.children or []:
            if child.type == "directory":
                contents.extend(await self._process_directory(child))
        return contents

    async def _process_directory(self, directory: FsItem) -> list[ContentItem]:
        """Process a directory that contains .cbz files as a comic series."""
        if not directory.children:
            return []

        cbz_files = [c for c in directory.children if c.type == "file" and c.path.suffix == ".cbz"]
        if not cbz_files:
            # If the directory contains files, skip it. Otherwise, recurse into
            # it.
            all_files = [c for c in directory.children if c.type == "file"]
            if all_files:
                return []
            else:
                return await self._process_children(directory)

        name, year = _parse_series_name(directory.path.name)
        children = [self._process_cbz(cbz) for cbz in sorted(cbz_files, key=lambda x: x.path.name)]
        series = ContentItem(
            uri_part=f"{name}_{year}" if year else name,
            type="comic_series",
            title=name,
            file_uri=directory.path.as_uri(),
            children=[child for child in children if child],
        )

        return [series]

    def _process_cbz(self, cbz: FsItem) -> ContentItem | None:
        """Create a Content entry for a single .cbz file."""
        filename = cbz.path.stem

        # Try to parse volume/chapter number
        vol_num = _parse_volume_number(filename)
        ch_num = _parse_chapter_number(filename)
        if vol_num is None and ch_num is None:
            ch_num = _parse_fallback_chapter_number(filename)

        parts = []
        if vol_num is not None:
            parts.append(f"Vol. {vol_num}")
        if ch_num is not None:
            parts.append(f"Ch. {ch_num}")
        title = " ".join(parts) if parts else None

        if not title:
            return None

        return ContentItem(
            uri_part=f"v{vol_num or 0}_ch{ch_num or 0}",
            type="comic",
            title=title,
            file_uri=cbz.path.as_uri(),
            file_modified_at=cbz.modified_at,
            order_parts=[vol_num or 0, ch_num or 0],
        )

    async def scan_item(self, item: ContentItem) -> None:
        assert item.content_inst
        pass


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
    As a last resort, parse the first number in the name as the chapter number.
    """
    if match := re.search(r"(\d+(?:\.\d+)?)", name):
        num = match.group(1)
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
    # Find all parenthesized groups and check from right to left
    matches = list(re.finditer(r"\((\d+)\)", name))
    for match in reversed(matches):
        year = int(match.group(1))
        if year >= 1000 and year <= 9999:
            return year


def _clean_series_name(name: str) -> str:
    """
    Removes any tags (in matching [] or ()) from the end of a series name, until none are
    left.
    """
    while True:
        # Remove trailing [] or () tags
        cleaned = re.sub(r"\s*[\[\(][^\[\]\(\)]*[\]\)]\s*$", "", name)
        if cleaned == name:
            break
        name = cleaned
    return name.strip()
