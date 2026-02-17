import pathlib
import shutil
import tempfile

import pytest
from sqlalchemy import select

from voltis.components.scanner.comics_scanner import ComicsScanner
from voltis.db.models import Content, ContentMetadataRow, Library, LibrarySource
from voltis.utils.misc import now_without_tz

TEST_DATA = pathlib.Path(__file__).resolve().parents[3] / "test_data" / "comics"


@pytest.fixture
async def library_with_comics(rb):
    with tempfile.TemporaryDirectory() as tmp:
        shutil.copytree(TEST_DATA, tmp, dirs_exist_ok=True)

        lib = Library(
            id=Library.make_id(),
            name="test-comics",
            type="comics",
            created_at=now_without_tz(),
            updated_at=now_without_tz(),
        )
        lib.set_sources([LibrarySource(path_uri=tmp)])

        async with rb.get_asession() as session:
            session.add(lib)
            await session.commit()

        yield rb, lib, tmp


@pytest.mark.anyio
async def test_comics_scan(library_with_comics):
    rb, lib, tmp = library_with_comics

    scanner = ComicsScanner(rb=rb, library=lib, events=False)
    result = await scanner.scan()

    assert len(result.added) == 6
    assert len(result.updated) == 0
    assert len(result.removed) == 0

    async with rb.get_asession() as session:
        all_content = list(
            (await session.scalars(select(Content).where(Content.library_id == lib.id))).all()
        )
        series = [c for c in all_content if c.type == "comic_series"]
        items = [c for c in all_content if c.type == "comic"]

        assert len(series) == 3
        assert len(items) == 6

        series_names = {s.uri_part for s in series}
        assert "Frieren" in series_names
        assert "Solo Leveling" in series_names

        # Frieren: 2 volumes, ordered
        frieren_series = next(s for s in series if s.uri_part == "Frieren")
        frieren_items = sorted(
            [i for i in items if i.parent_id == frieren_series.id], key=lambda c: c.order
        )
        assert len(frieren_items) == 2
        assert frieren_items[0].uri_part == "v1"
        assert frieren_items[1].uri_part == "v2"

        # Solo Leveling: 3 chapters, ordered
        sl_series = next(s for s in series if s.uri_part == "Solo Leveling")
        sl_items = sorted([i for i in items if i.parent_id == sl_series.id], key=lambda c: c.order)
        assert len(sl_items) == 3
        assert sl_items[0].uri_part == "ch1"
        assert sl_items[1].uri_part == "ch2"
        assert sl_items[2].uri_part == "ch3"

        # One Punch Man: year-based
        opm_series = next(s for s in series if "One Punch Man" in s.uri_part)
        opm_items = [i for i in items if i.parent_id == opm_series.id]
        assert len(opm_items) == 1
        assert "y2025" in opm_items[0].uri_part

        # Cover URIs set
        for item in items:
            assert item.cover_uri is not None

        # Pages in file_data
        for item in items:
            fd = item.mutate_file_data()
            assert len(fd["pages"]) > 0

        # Metadata: Frieren has manga=Yes
        frieren_meta = (
            await session.scalars(
                select(ContentMetadataRow).where(
                    ContentMetadataRow.uri == frieren_items[0].uri,
                    ContentMetadataRow.library_id == lib.id,
                )
            )
        ).first()
        assert frieren_meta is not None
        assert frieren_meta.data.get("manga") == "Yes"
        assert frieren_meta.data.get("series") == "Frieren"

    # Re-scan: idempotent
    scanner2 = ComicsScanner(rb=rb, library=lib, events=False)
    result2 = await scanner2.scan()
    assert len(result2.added) == 0
    assert len(result2.updated) == 0
    assert len(result2.removed) == 0
    assert len(result2.unchanged) == 6

    # Modify files and re-scan
    frieren_dir = pathlib.Path(tmp) / "Frieren"
    (frieren_dir / "Frieren v01.cbz").rename(frieren_dir / "Frieren v01 (some tag).cbz")

    sl_dir = pathlib.Path(tmp) / "Solo Leveling"
    shutil.copy(sl_dir / "Solo Leveling c03.cbz", sl_dir / "Solo Leveling c04.cbz")

    scanner3 = ComicsScanner(rb=rb, library=lib, events=False)
    result3 = await scanner3.scan()

    # Renamed file: old removed + new added; new chapter added
    assert len(result3.added) == 2
    assert len(result3.removed) == 1

    async with rb.get_asession() as session:
        all_content = list(
            (await session.scalars(select(Content).where(Content.library_id == lib.id))).all()
        )
        items = [c for c in all_content if c.type == "comic"]
        series = [c for c in all_content if c.type == "comic_series"]

        assert len(items) == 7
        assert len(series) == 3

        # Frieren v01 still exists with same uri_part, but new file_uri
        frieren_series = next(s for s in series if s.uri_part == "Frieren")
        frieren_items = sorted(
            [i for i in items if i.parent_id == frieren_series.id], key=lambda c: c.order
        )
        assert len(frieren_items) == 2
        assert frieren_items[0].uri_part == "v1"
        assert "some tag" in frieren_items[0].file_uri

        # Solo Leveling now has 4 chapters
        sl_series = next(s for s in series if s.uri_part == "Solo Leveling")
        sl_items = sorted([i for i in items if i.parent_id == sl_series.id], key=lambda c: c.order)
        assert len(sl_items) == 4
        assert sl_items[3].uri_part == "ch4"
