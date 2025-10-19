import pytest
from anyio import Path

from voltis.components.scanner.base import FoundItem
from voltis.components.scanner.comics import ComicScanner
from voltis.db.models import DataSource


def create_mock_structure():
    return [
        FoundItem(
            type="directory",
            path=Path("/library"),
            children=[
                FoundItem(
                    type="directory",
                    path=Path("/library/Series Name (2020)"),
                    children=[
                        FoundItem(
                            type="file",
                            path=Path("/library/Series Name (2020)/Series Name (2020) #01.cbz"),
                            children=None,
                        ),
                        FoundItem(
                            type="file",
                            path=Path("/library/Series Name (2020)/Series Name (2020) #02.cbz"),
                            children=None,
                        ),
                        FoundItem(
                            type="file",
                            path=Path("/library/Series Name (2020)/Series Name (2020) #03.cbz"),
                            children=None,
                        ),
                    ],
                ),
                FoundItem(
                    type="directory",
                    path=Path("/library/Series Name 2 (2021)"),
                    children=[
                        FoundItem(
                            type="file",
                            path=Path("/library/Series Name 2 (2021)/Series Name 2 #01.cbz"),
                            children=None,
                        ),
                        FoundItem(
                            type="file",
                            path=Path("/library/Series Name 2 (2021)/Series Name 2 #02.cbz"),
                            children=None,
                        ),
                        FoundItem(
                            type="file",
                            path=Path("/library/Series Name 2 (2021)/Series Name 2 #102.cbz"),
                            children=None,
                        ),
                    ],
                ),
                FoundItem(
                    type="directory",
                    path=Path("/library/Some Other Folder"),
                    children=[
                        FoundItem(
                            type="directory",
                            path=Path("/library/Some Other Folder/Series Name 2 (2021)"),
                            children=[
                                FoundItem(
                                    type="file",
                                    path=Path(
                                        "/library/Some Other Folder/Series Name 2 (2021)/Series Name 2 #01.cbz"
                                    ),
                                    children=None,
                                ),
                                FoundItem(
                                    type="file",
                                    path=Path(
                                        "/library/Some Other Folder/Series Name 2 (2021)/Series Name 2 #02.cbz"
                                    ),
                                    children=None,
                                ),
                                FoundItem(
                                    type="file",
                                    path=Path(
                                        "/library/Some Other Folder/Series Name 2 (2021)/Series Name 2 #03.cbz"
                                    ),
                                    children=None,
                                ),
                            ],
                        )
                    ],
                ),
            ],
        )
    ]


@pytest.mark.anyio
async def test_comic_scanner_example_structure(rb):
    """Test that the comic scanner correctly parses the example directory structure."""
    scanner = ComicScanner(
        rb,
        DataSource(
            id="test-id",
            path="/library",
        ),
    )
    root = create_mock_structure()
    contents = await scanner.scan_items(root)

    for c in contents:
        print(c)

    # Filter series and comics
    comic_series = [c for c in contents if c.type == "comic_series"]
    comics = [c for c in contents if c.type == "comic"]

    assert len(comic_series) == 3
    assert len(comics) == 9

    # Check series titles
    series_titles = {s.title for s in comic_series}
    assert series_titles == {"Series Name", "Series Name 2"}

    # Check that all comics have a parent
    assert all(c.parent_id is not None for c in comics)

    # Check that all comic parent_ids reference a series
    series_ids = {s.id for s in comic_series}
    assert all(c.parent_id in series_ids for c in comics)

    # Check specific series
    series_name_2020 = next(s for s in comic_series if s.title == "Series Name")
    assert series_name_2020.content_id == "Series Name_2020"

    # Check that Series Name has 3 children
    series_name_comics = [c for c in comics if c.parent_id == series_name_2020.id]
    assert len(series_name_comics) == 3

    # Check order_parts for proper sorting
    assert all(c.order_parts == [i + 1, 0] for i, c in enumerate(series_name_comics))

    # Check titles are generated correctly
    assert series_name_comics[0].title == "Vol. 1"
    assert series_name_comics[1].title == "Vol. 2"
    assert series_name_comics[2].title == "Vol. 3"
