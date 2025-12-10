import datetime

import pytest
from sqlalchemy import select

from voltis.components.scanner.base import LibraryFile
from voltis.components.scanner.comics_scanner import (
    ComicScanner,
    _clean_series_name,
    _natural_sort_key,
    _parse_chapter_number,
    _parse_fallback_chapter_number,
    _parse_series_name,
    _parse_series_year,
    _parse_volume_number,
)
from voltis.db.models import Content, Library
from voltis.services.resource_broker import ResourceBroker
from voltis.utils.misc import now_without_tz


def create_mock_files() -> list[LibraryFile]:
    """Create LibraryFile objects representing a mock filesystem structure."""
    base_time = datetime.datetime(2024, 1, 1, 12, 0, 0)
    return [
        # Series Name (2020), 3 issues
        LibraryFile(
            uri="file:///library/Series%20Name%20(2020)/Series%20Name%20(2020)%20%2301.cbz",
            mtime=base_time,
            size=1000,
        ),
        LibraryFile(
            uri="file:///library/Series%20Name%20(2020)/Series%20Name%20(2020)%20%2302.cbz",
            mtime=base_time,
            size=1000,
        ),
        LibraryFile(
            uri="file:///library/Series%20Name%20(2020)/Series%20Name%20(2020)%20%2303.cbz",
            mtime=base_time,
            size=1000,
        ),
        # Series Name 2 (2021), 3 issues
        LibraryFile(
            uri="file:///library/Series%20Name%202%20(2021)/Series%20Name%202%20%2301.cbz",
            mtime=base_time,
            size=1000,
        ),
        LibraryFile(
            uri="file:///library/Series%20Name%202%20(2021)/Series%20Name%202%20%2302.cbz",
            mtime=base_time,
            size=1000,
        ),
        LibraryFile(
            uri="file:///library/Series%20Name%202%20(2021)/Series%20Name%202%20%23102.cbz",
            mtime=base_time,
            size=1000,
        ),
        # Same series in different folder, 3 issues
        LibraryFile(
            uri="file:///library/Some%20Other%20Folder/Series%20Name%202%20(2021)/Series%20Name%202%20%2303.cbz",
            mtime=base_time,
            size=1000,
        ),
        LibraryFile(
            uri="file:///library/Some%20Other%20Folder/Series%20Name%202%20(2021)/Series%20Name%202%20%2304.cbz",
            mtime=base_time,
            size=1000,
        ),
        LibraryFile(
            uri="file:///library/Some%20Other%20Folder/Series%20Name%202%20(2021)/Series%20Name%202%20%2305.cbz",
            mtime=base_time,
            size=1000,
        ),
    ]


@pytest.mark.anyio
async def test_comic_scanner_example_structure(rb: ResourceBroker):
    """Test that the comic scanner correctly parses the example directory structure."""
    # Create library
    library = Library(
        id=Library.make_id(),
        name="Test Library",
        type="comics",
        sources=[{"path_uri": "file:///library"}],
        created_at=now_without_tz(),
        updated_at=now_without_tz(),
    )
    async with rb.get_asession() as session:
        session.add(library)
        await session.commit()

    # Scan
    scanner = ComicScanner(library, rb, no_fs=True)
    result = await scanner.scan_direct(create_mock_files())

    assert len(result.added) == 9

    # Query database for series
    async with rb.get_asession() as session:
        series_result = await session.scalars(
            select(Content).where(
                Content.library_id == library.id,
                Content.type == "comic_series",
            )
        )
        series_list = series_result.all()

        # Should have 2 series (Series Name 2 in different folders should be merged)
        assert len(series_list) == 2

        series_titles = {s.title for s in series_list}
        assert series_titles == {"Series Name", "Series Name 2"}

        # Check Series Name (2020)
        series_name = next(s for s in series_list if s.title == "Series Name")
        assert series_name.uri_part == "Series Name_2020"

        # Query children of Series Name
        children_result = await session.execute(
            select(Content).where(Content.parent_id == series_name.id)
        )
        children = list(children_result.scalars().all())

        assert len(children) == 3
        children.sort(key=lambda c: c.order_parts)
        assert children[0].title == "Vol. 1"
        assert children[1].title == "Vol. 2"
        assert children[2].title == "Vol. 3"

        # Check Series Name 2 has 6 children (merged from two folders)
        series_name_2 = next(s for s in series_list if s.title == "Series Name 2")
        children_result = await session.execute(
            select(Content).where(Content.parent_id == series_name_2.id)
        )
        children_2 = list(children_result.scalars().all())

        assert len(children_2) == 6

    # Re-scan with modified files
    base_time = datetime.datetime(2024, 1, 1, 12, 0, 0)
    updated_files = [
        # Keep only #102 from Series Name 2
        LibraryFile(
            uri="file:///library/Series%20Name%202%20(2021)/Series%20Name%202%20%23102.cbz",
            mtime=base_time,
            size=1000,
        ),
        # New series
        LibraryFile(
            uri="file:///library/New%20Series%20(2024)/New%20Series%20%2301.cbz",
            mtime=base_time,
            size=1000,
        ),
    ]

    scanner2 = ComicScanner(library, rb, no_fs=True)
    result2 = await scanner2.scan_direct(updated_files)

    assert len(result2.added) == 1  # New Series #01
    assert len(result2.removed) == 8  # 3 from Series Name + 5 from Series Name 2

    async with rb.get_asession() as session:
        # Series Name should be deleted (no children)
        series_result = await session.scalars(
            select(Content).where(
                Content.library_id == library.id,
                Content.type == "comic_series",
            )
        )
        series_list = series_result.all()

        assert {s.title for s in series_list} == {"Series Name 2", "New Series"}

        # Check Series Name 2 now has only 1 child
        series_name_2 = next(s for s in series_list if s.title == "Series Name 2")
        children_result = await session.scalars(
            select(Content).where(Content.parent_id == series_name_2.id)
        )
        children_2 = children_result.all()
        assert len(children_2) == 1
        assert children_2[0].title == "Vol. 102"

        # Check New Series has 1 child
        new_series = next(s for s in series_list if s.title == "New Series")
        assert new_series.uri_part == "New Series_2024"
        children_result = await session.scalars(
            select(Content).where(Content.parent_id == new_series.id)
        )
        new_children = children_result.all()
        assert len(new_children) == 1
        assert new_children[0].title == "Vol. 1"


def test_utilities():
    assert _parse_volume_number("Series Name #01") == 1
    assert _parse_volume_number("Series Name #12") == 12
    assert _parse_volume_number("Series Name v01") == 1
    assert _parse_volume_number("Series Name v.01") == 1
    assert _parse_volume_number("Series Name vol.1") == 1
    assert _parse_volume_number("Series Name Vol.12") == 12
    assert _parse_volume_number("Series Name v1.5") == 1.5
    assert _parse_volume_number("Series Name #2.5") == 2.5
    assert _parse_volume_number("Series Name") is None
    assert _parse_volume_number("Series Name Chapter 1") is None

    assert _parse_chapter_number("Series Name c01") == 1
    assert _parse_chapter_number("Series Name ch01") == 1
    assert _parse_chapter_number("Series Name ch.01") == 1
    assert _parse_chapter_number("Series Name chap.01") == 1
    assert _parse_chapter_number("Series Name Ch12") == 12
    assert _parse_chapter_number("Series Name ch1.5") == 1.5
    assert _parse_chapter_number("Series Name") is None
    assert _parse_chapter_number("Series Name #01") is None

    assert _parse_fallback_chapter_number("001") == 1
    assert _parse_fallback_chapter_number("Series Name 01") == 1
    assert _parse_fallback_chapter_number("12 - Title") == 12
    assert _parse_fallback_chapter_number("1.5") == 1.5
    assert _parse_fallback_chapter_number("No numbers here") is None

    assert _parse_series_year("My Series (2020)") == 2020
    assert _parse_series_year("My Series (1999)") == 1999
    assert _parse_series_year("My Series (90) (2020)") == 2020
    assert _parse_series_year("My Series (202A)") is None
    assert _parse_series_year("My Series (90)") is None
    assert _parse_series_year("My Series (202)") is None
    assert _parse_series_year("My Series") is None
    assert _clean_series_name("My Series (2020)") == "My Series"
    assert _clean_series_name("My Series (2020) (something)") == "My Series"
    assert _clean_series_name("My Series [tag]") == "My Series"
    assert _clean_series_name("My Series [tag 1] [tag 2]") == "My Series"
    assert _clean_series_name("My Series (2020) [tag]") == "My Series"
    assert _clean_series_name("My (Special) Series (2020)") == "My (Special) Series"
    assert _clean_series_name("My Series") == "My Series"

    assert _parse_series_name("My Series (2020)") == ("My Series", 2020)
    assert _parse_series_name("My Series") == ("My Series", None)
    assert _parse_series_name("My Series (2020) (something else)") == ("My Series", 2020)
    assert _parse_series_name("My Series (Specials) [tag 1] [tag 2]") == (
        "My Series",
        None,
    )
    assert _parse_series_name("My Series (202A)") == ("My Series", None)

    assert sorted(["page10", "page2", "page1", "page20"], key=_natural_sort_key) == [
        "page1",
        "page2",
        "page10",
        "page20",
    ]
    assert sorted(["ch1/page10.jpg", "ch1/page2.jpg", "ch2/page1.jpg"], key=_natural_sort_key) == [
        "ch1/page2.jpg",
        "ch1/page10.jpg",
        "ch2/page1.jpg",
    ]
    assert sorted(["Page10", "page2", "PAGE1"], key=_natural_sort_key) == [
        "PAGE1",
        "page2",
        "Page10",
    ]
