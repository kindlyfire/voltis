import click
import structlog
from sqlalchemy import select
from sqlalchemy.dialects.postgresql import insert as pg_insert

from voltis.components.metadata_sources.mangabaka import MangaBaka, Series
from voltis.db.models import Content, ContentMetadataDict, ContentMetadataRow, Library
from voltis.services.resource_broker import ResourceBroker
from voltis.utils.misc import now_without_tz

logger = structlog.stdlib.get_logger()

PROVIDER_MANGABAKA = 2


def _map_to_metadata(s: Series) -> ContentMetadataDict:
    d = ContentMetadataDict()
    if authors := s.get("authors"):
        d["authors"] = authors
    if desc := s.get("description"):
        d["description"] = desc
    if genres := s.get("genres"):
        d["genre"] = ", ".join(genres)
    if year := s.get("year"):
        d["publication_date"] = str(year)
    if pubs := s.get("publishers"):
        for p in pubs:
            if "name" in p and p["name"]:
                d["publisher"] = p["name"]
                break
    return d


async def _scan_sources(
    rb: ResourceBroker,
    library_id: str,
    content_id: str | None,
    force: bool,
):
    async with rb.get_asession() as session:
        lib = await session.scalar(select(Library).where(Library.id == library_id))
        if not lib:
            click.echo(f"Error: Library {library_id} not found", err=True)
            return

        # Load comic series
        q = select(Content).where(
            Content.library_id == library_id,
            Content.type == "comic_series",
        )
        if content_id:
            q = q.where(Content.id == content_id)
        series_list = list((await session.scalars(q)).all())

        if not series_list:
            click.echo("No comic series found.")
            return

        # Build skip set from existing metadata
        skip_uris: set[str] = set()
        if not force:
            existing = (
                await session.scalars(
                    select(ContentMetadataRow.uri).where(
                        ContentMetadataRow.library_id == library_id,
                        ContentMetadataRow.provider == PROVIDER_MANGABAKA,
                        ContentMetadataRow.remote_id.isnot(None),
                    )
                )
            ).all()
            skip_uris = set(existing)

        matched = 0
        skipped = 0
        not_found = 0

        async with MangaBaka() as mb:
            for content in series_list:
                if content.uri in skip_uris:
                    logger.info("skipping (already matched)", series=content.uri_part)
                    skipped += 1
                    continue

                logger.info("searching", series=content.uri_part)
                try:
                    results = await mb.series_search(q=content.uri_part)
                except Exception:
                    logger.exception("search failed", series=content.uri_part)
                    continue

                if not results["data"]:
                    logger.warning("no results", series=content.uri_part)
                    not_found += 1
                    continue

                hit = results["data"][0]
                assert "id" in hit
                try:
                    full = await mb.series_get(hit["id"])
                    assert "id" in full
                except Exception:
                    logger.exception("fetch failed", series=content.uri_part, mb_id=hit["id"])
                    continue

                logger.info(
                    "matched",
                    series=content.uri_part,
                    mb_title=full.get("title"),
                    mb_id=full["id"],
                )

                row_dict = {
                    "uri": content.uri,
                    "library_id": library_id,
                    "provider": PROVIDER_MANGABAKA,
                    "remote_id": str(full["id"]),
                    "data": dict(_map_to_metadata(full)),
                    "raw": dict(full),
                    "updated_at": now_without_tz(),
                }
                stmt = pg_insert(ContentMetadataRow).values(row_dict)
                stmt = stmt.on_conflict_do_update(
                    index_elements=["uri", "library_id", "provider"],
                    set_={
                        "remote_id": stmt.excluded.remote_id,
                        "data": stmt.excluded.data,
                        "raw": stmt.excluded.raw,
                        "updated_at": stmt.excluded.updated_at,
                    },
                )
                await session.execute(stmt)
                matched += 1

        await session.commit()

    click.echo(f"\nDone: {matched} matched, {skipped} skipped, {not_found} not found")
