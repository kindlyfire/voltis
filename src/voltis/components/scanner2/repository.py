import structlog
from sqlalchemy import delete, select
from sqlalchemy import inspect as sa_inspect
from sqlalchemy.dialects.postgresql import insert as pg_insert
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy.orm.attributes import flag_modified

from voltis.db.models import Content, ContentMetadataRow, ContentType, GroupingContentTypes
from voltis.services.resource_broker import ResourceBroker
from voltis.utils.misc import now_without_tz

logger = structlog.stdlib.get_logger()


def _is_dirty(obj) -> bool:
    """Check if an ORM object is new (transient) or has modified attributes."""
    state = sa_inspect(obj)
    if state.transient:
        return True
    return any(state.attrs[attr.key].history.has_changes() for attr in state.mapper.column_attrs)


class ScannerRepository:
    """
    We load all the data the scanner needs from the database here, keep track of
    it, and update everything at the end. Also has some handy utils.
    """

    def __init__(self, rb: ResourceBroker, library_id: str):
        self.rb = rb
        self.library_id = library_id
        self.content: list[Content] = []
        self.content_d: list[Content] = []
        self.content_metadata: list[ContentMetadataRow] = []
        self.content_metadata_d: list[ContentMetadataRow] = []

        self.resolved_parents: dict[str, Content] = {}

    async def load(self):
        async with self.rb.get_asession() as session:
            content_r = await session.scalars(
                select(Content).where(Content.library_id == self.library_id)
            )
            self.content = list(content_r.all())

            metadata = await session.scalars(
                select(ContentMetadataRow).where(ContentMetadataRow.library_id == self.library_id)
            )
            self.content_metadata = list(metadata.all())

    async def commit(self, session: AsyncSession):
        """Save any changes made to the database."""

        # Delete removed content
        if self.content_d:
            await session.execute(
                delete(Content).where(Content.id.in_([c.id for c in self.content_d]))
            )

        # Delete orphaned series (no children)
        parent_ids = {c.parent_id for c in self.content if c.parent_id}
        orphans = [
            c for c in self.content if c.type in GroupingContentTypes and c.id not in parent_ids
        ]
        if orphans:
            await session.execute(delete(Content).where(Content.id.in_([c.id for c in orphans])))
            for c in orphans:
                self.content.remove(c)

        # Upsert modified content
        content_rows = [c.as_dict() for c in self.content if _is_dirty(c)]
        if content_rows:
            content_update_cols = [
                "uri_part",
                "uri",
                "valid",
                "file_uri",
                "file_mtime",
                "file_size",
                "cover_uri",
                "type",
                "order",
                "order_parts",
                "file_data",
                "parent_id",
                "updated_at",
            ]
            stmt = pg_insert(Content).values(content_rows)
            stmt = stmt.on_conflict_do_update(
                index_elements=["id"],
                set_={col: stmt.excluded[col] for col in content_update_cols},
            )
            await session.execute(stmt)

        # Upsert modified metadata
        meta_rows = [m.as_dict() for m in self.content_metadata if _is_dirty(m)]
        if meta_rows:
            stmt = pg_insert(ContentMetadataRow).values(meta_rows)
            stmt = stmt.on_conflict_do_update(
                index_elements=["uri", "library_id", "provider"],
                set_={
                    "data": stmt.excluded.data,
                    "raw": stmt.excluded.raw,
                    "updated_at": stmt.excluded.updated_at,
                },
            )
            await session.execute(stmt)

        # Delete removed metadata
        if self.content_metadata_d:
            for m in self.content_metadata_d:
                await session.execute(
                    delete(ContentMetadataRow).where(
                        ContentMetadataRow.uri == m.uri,
                        ContentMetadataRow.library_id == m.library_id,
                        ContentMetadataRow.provider == m.provider,
                    )
                )

    def match_deleted_item(self, uri_part: str, parent_id: str | None) -> Content | None:
        """
        If an item is new, we try to find a deleted item with the same uri_part
        and parent. This is needed because the initial matching is only done by
        file path.
        """
        item = next(
            (v for v in self.content_d if v.uri_part == uri_part and v.parent_id == parent_id),
            None,
        )
        if item:
            self.content_d.remove(item)
            self.content.append(item)
        return item

    def get_series(
        self,
        *,
        uri: str,
        uri_part: str,
        file_uri: str | None,
        type: ContentType,
        title: str,
    ):
        """Find an existing series or create a new one based on the folder."""
        if uri in self.resolved_parents:
            return self.resolved_parents[uri]

        item = next(
            (
                v
                for v in self.content
                if v.uri == uri or (file_uri is not None and v.file_uri == file_uri)
            ),
            None,
        )
        if item:
            self.resolved_parents[uri] = item
            return item

        item = Content(
            id=Content.make_id(),
            library_id=self.library_id,
            uri_part=uri_part,
            uri=uri,
            type=type,
            file_uri=file_uri,
            order_parts=[],
            created_at=now_without_tz(),
            updated_at=now_without_tz(),
        )
        self.content.append(item)
        self.get_metadata(uri=uri, provider=0).data = {"title": title}
        self.resolved_parents[uri] = item
        return item

    def get_metadata(self, uri: str, provider: int) -> ContentMetadataRow:
        """Get metadata for a given content item and provider."""
        m = next(
            (m for m in self.content_metadata if m.uri == uri and m.provider == provider), None
        )
        if not m:
            m = ContentMetadataRow(
                uri=uri,
                library_id=self.library_id,
                provider=provider,
                updated_at=now_without_tz(),
                data={},
                raw={},
            )
            self.content_metadata.append(m)
        flag_modified(m, "data")
        flag_modified(m, "raw")
        return m
