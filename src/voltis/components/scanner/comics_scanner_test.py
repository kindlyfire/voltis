import pytest
from anyio import Path

from voltis.components.scanner.base import FsItem
from voltis.components.scanner.comics_scanner import ComicScanner


def create_mock_structure():
    return [
        FsItem(
            type="directory",
            path=Path("/library"),
            children=[
                FsItem(
                    type="directory",
                    path=Path("/library/Series Name (2020)"),
                    children=[
                        FsItem(
                            type="file",
                            path=Path("/library/Series Name (2020)/Series Name (2020) #01.cbz"),
                            children=None,
                        ),
                        FsItem(
                            type="file",
                            path=Path("/library/Series Name (2020)/Series Name (2020) #02.cbz"),
                            children=None,
                        ),
                        FsItem(
                            type="file",
                            path=Path("/library/Series Name (2020)/Series Name (2020) #03.cbz"),
                            children=None,
                        ),
                    ],
                ),
                FsItem(
                    type="directory",
                    path=Path("/library/Series Name 2 (2021)"),
                    children=[
                        FsItem(
                            type="file",
                            path=Path("/library/Series Name 2 (2021)/Series Name 2 #01.cbz"),
                            children=None,
                        ),
                        FsItem(
                            type="file",
                            path=Path("/library/Series Name 2 (2021)/Series Name 2 #02.cbz"),
                            children=None,
                        ),
                        FsItem(
                            type="file",
                            path=Path("/library/Series Name 2 (2021)/Series Name 2 #102.cbz"),
                            children=None,
                        ),
                    ],
                ),
                FsItem(
                    type="directory",
                    path=Path("/library/Some Other Folder"),
                    children=[
                        FsItem(
                            type="directory",
                            path=Path("/library/Some Other Folder/Series Name 2 (2021)"),
                            children=[
                                FsItem(
                                    type="file",
                                    path=Path(
                                        "/library/Some Other Folder/Series Name 2 (2021)/Series Name 2 #01.cbz"
                                    ),
                                    children=None,
                                ),
                                FsItem(
                                    type="file",
                                    path=Path(
                                        "/library/Some Other Folder/Series Name 2 (2021)/Series Name 2 #02.cbz"
                                    ),
                                    children=None,
                                ),
                                FsItem(
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
async def test_comic_scanner_example_structure():
    """Test that the comic scanner correctly parses the example directory structure."""
    scanner = ComicScanner()
    root = create_mock_structure()
    contents = await scanner.scan_items(root)

    for c in contents:
        print(c)

    # All top-level items should be series
    assert all(c.type == "comic_series" for c in contents)
    assert len(contents) == 3

    # Count total comics across all series
    total_comics = sum(len(s.children) for s in contents)
    assert total_comics == 9

    # Check series titles
    series_titles = {s.title for s in contents}
    assert series_titles == {"Series Name", "Series Name 2"}

    # Check that all series have children
    assert all(len(s.children) > 0 for s in contents)

    # Check specific series
    series_name_2020 = next(s for s in contents if s.title == "Series Name")
    assert series_name_2020.uri_part == "Series Name_2020"

    # Check that Series Name has 3 children
    assert len(series_name_2020.children) == 3

    # Check order_parts for proper sorting
    assert all(c.order_parts == [i + 1, 0] for i, c in enumerate(series_name_2020.children))

    # Check titles are generated correctly
    assert series_name_2020.children[0].title == "Vol. 1"
    assert series_name_2020.children[1].title == "Vol. 2"
    assert series_name_2020.children[2].title == "Vol. 3"
