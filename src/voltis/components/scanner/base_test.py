import pytest
from sqlalchemy import select

from voltis.components.scanner.base import ContentItem, FsItem, ScannerBase
from voltis.db.models import Content, DataSource


class _TestScanner(ScannerBase):
    """Minimal scanner implementation for testing."""

    async def scan_items(self, items: list[FsItem]) -> list[ContentItem]:
        return []

    async def scan_item(self, item: ContentItem) -> None:
        pass


@pytest.mark.anyio
async def test_match_items(rb):
    """Test that _match_items correctly matches ContentItems to existing Content."""
    ds = DataSource(
        id="test-datasource",
        path_uri="file:///test/path",
    )
    scanner = _TestScanner()

    items = [
        ContentItem(
            uri_part="series1",
            title="Series 1",
            type="comic_series",
            children=[
                ContentItem(uri_part="issue1", title="Issue 1", type="comic"),
                ContentItem(uri_part="issue2", title="Issue 2", type="comic"),
                ContentItem(uri_part="issue3", title="Issue 3", type="comic"),
            ],
        ),
        ContentItem(uri_part="series2", title="Series 2", type="comic_series"),
    ]

    existing_contents = [
        Content(
            id="s1",
            uri_part="series1",
            title="Series 1",
            type="comic_series",
            datasource_id=ds.id,
            parent_id=None,
        ),
        Content(
            id="i1",
            uri_part="issue1",
            title="Issue 1",
            type="comic",
            datasource_id=ds.id,
            parent_id="s1",
        ),
        Content(
            id="i2",
            uri_part="issue2",
            title="Issue 2 (old)",
            type="comic",
            datasource_id=ds.id,
            parent_id="s1",
        ),
        Content(
            id="i4",
            uri_part="issue4",
            title="Issue 4 (to be deleted)",
            type="comic",
            datasource_id=ds.id,
            parent_id="s1",
        ),
        Content(
            id="old",
            uri_part="old_series",
            title="Old Series",
            type="comic_series",
            datasource_id=ds.id,
            parent_id=None,
        ),
    ]

    to_delete = await scanner.match_from_instances(items, existing_contents)

    # Check series1 matched existing content
    assert items[0].content_inst is not None
    assert items[0].content_inst.id == "s1"
    assert items[0].content_new is False

    # Check issue1 matched
    assert items[0].children[0].content_inst
    assert items[0].children[0].content_inst.id == "i1"
    assert items[0].children[0].content_new is False

    # Check issue2 matched
    assert items[0].children[1].content_inst
    assert items[0].children[1].content_inst.id == "i2"
    assert items[0].children[1].content_new is False

    # Check issue3 is new
    assert items[0].children[2].content_inst
    assert items[0].children[2].content_inst.id is not None
    assert items[0].children[2].content_new is True

    # Check series2 is new
    assert items[1].content_inst is not None
    assert items[1].content_new is True

    # Check old_series is marked for deletion
    assert len(to_delete) == 2
    assert to_delete[0].uri_part == "old_series"
    assert to_delete[1].uri_part == "issue4"


@pytest.mark.anyio
async def test_save_and_update(rb):
    """Test saving items to the database and updating them."""

    ds = DataSource(id="test-datasource", path_uri="file:///test/path", type="comics")
    scanner = _TestScanner()

    # Initial items: two series with two entries each
    items = [
        ContentItem(
            uri_part="series1",
            title="Series 1",
            type="comic_series",
            children=[
                ContentItem(
                    uri_part="issue1",
                    title="Issue 1",
                    type="comic",
                    order_parts=[1, 0],
                ),
                ContentItem(
                    uri_part="issue2",
                    title="Issue 2",
                    type="comic",
                    order_parts=[2, 0],
                ),
            ],
        ),
        ContentItem(
            uri_part="series2",
            title="Series 2",
            type="comic_series",
            children=[
                ContentItem(
                    uri_part="issue2",
                    title="Issue 2 series2",
                    type="comic",
                    order_parts=[2, 0],
                ),
                ContentItem(
                    uri_part="issue1",
                    title="Issue 1 series2",
                    type="comic",
                    order_parts=[1, 0],
                ),
            ],
        ),
    ]

    # Match and save (all new)
    async with rb.get_asession() as session:
        session.add(ds)
        await session.commit()

        to_delete = await scanner.match_from_db(session, ds.id, items)
        assert to_delete == []
        await scanner.save(session, ds.id, items, to_delete)

        contents = (await session.scalars(select(Content))).all()
        assert len(contents) == 6  # 2 series + 4 issues

        series1_issue1 = next(
            c
            for c in contents
            if c.title == "Issue 1"
            and items[0].content_inst
            and c.parent_id == items[0].content_inst.id
        )

        # Modify series1: remove issue2, add issue3
        items[0].children = [
            items[0].children[0],  # Keep issue1
            ContentItem(uri_part="issue3", title="Issue 3", type="comic", order_parts=[3, 0]),
        ]

        # Match and save again
        to_delete = await scanner.match_from_db(session, ds.id, items)
        assert len(to_delete) == 1
        assert to_delete[0].uri_part == "issue2"
        await scanner.save(session, ds.id, items, to_delete)

        contents = (await session.scalars(select(Content))).all()
        assert len(contents) == 6  # 2 series + 4 issues (issue2 deleted, issue3 added)

        series1_contents = [
            c for c in contents if items[0].content_inst and c.parent_id == items[0].content_inst.id
        ]
        assert len(series1_contents) == 2
        assert {c.uri_part for c in series1_contents} == {"issue1", "issue3"}

        # Check that issue1's updated_at hasn't changed
        series1_issue1_updated = next(
            c
            for c in contents
            if c.title == "Issue 1"
            and items[0].content_inst
            and c.parent_id == items[0].content_inst.id
        )
        assert series1_issue1.updated_at == series1_issue1_updated.updated_at
        assert series1_issue1_updated.order == 0

        # Check the order
        title_orders = {
            "Issue 1": 0,
            "Issue 3": 1,
            "Issue 1 series2": 0,
            "Issue 2 series2": 1,
        }
        for c in contents:
            if c.parent_id is not None:
                assert c.order == title_orders[c.title]
