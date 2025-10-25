import datetime
from typing import Any, Literal
from uuid import uuid4

from sqlalchemy import (
    ARRAY,
    REAL,
    ForeignKey,
    Text,
)
from sqlalchemy.dialects.postgresql import JSONB, TIMESTAMP
from sqlalchemy.inspection import inspect
from sqlalchemy.orm import DeclarativeBase, Mapped, relationship
from sqlalchemy.orm import mapped_column as col

DataSourceType = Literal["comics", "books"]
ContentType = Literal["comic", "comic_series", "book", "book_series"]


class _Base(DeclarativeBase):
    def as_dict(self):
        return {c.key: getattr(self, c.key) for c in inspect(self).mapper.column_attrs}

    def __repr__(self):
        d = self.as_dict()
        d_str = ", ".join(f"{k}={v!r}" for k, v in d.items())
        return f"<{self.__class__.__name__} {d_str}>"

    @classmethod
    def make_id(cls) -> str:
        if not hasattr(cls, "__idprefix__"):
            raise NotImplementedError("gen_id requires __idprefix__ to be set")
        return f"{getattr(cls, '__idprefix__')}_{uuid4().hex}"


class _DefaultColumns:
    id: Mapped[str] = col(Text, primary_key=True)
    created_at: Mapped[datetime.datetime] = col(TIMESTAMP, server_default="")
    updated_at: Mapped[datetime.datetime] = col(
        TIMESTAMP,
        server_default="",
        onupdate=datetime.datetime.utcnow,
    )


class User(_Base, _DefaultColumns):
    __tablename__ = "users"
    __idprefix__ = "u"

    username: Mapped[str] = col(Text, unique=True)
    password_hash: Mapped[str] = col(Text)
    permissions: Mapped[list[str]] = col(ARRAY(Text), server_default="")

    sessions: Mapped[list["Session"]] = relationship(back_populates="user")


class Session(_Base):
    __tablename__ = "sessions"
    __idprefix__ = "s"

    token: Mapped[str] = col(Text, primary_key=True)
    user_id: Mapped[str] = col(Text, ForeignKey("users.id"))

    user: Mapped["User"] = relationship(back_populates="sessions")


class DataSource(_Base, _DefaultColumns):
    """
    A data source represents a folder on disk that contains books or comics.

    Right now, only folders are supported, but I would like to add support for
    S3. And maybe native rclone support.
    """

    __tablename__ = "data_sources"
    __idprefix__ = "ds"

    path_uri: Mapped[str] = col(Text)
    type: Mapped[DataSourceType] = col(Text)
    scanned_at: Mapped[datetime.datetime | None] = col(TIMESTAMP)

    contents: Mapped[list["Content"]] = relationship(back_populates="datasource")


class Content(_Base, _DefaultColumns):
    """
    Individual pieces of content as well as groups of content (e.g. a series)
    each have a line in this table, in a tree-like structure.

    The potential tree structures are as follows:

    - Book
    - Book series -> Book
    - Comic series -> Comic volume
    - Comic series -> Comic issue
    - Comic series -> Specials -> Comic issue
    """

    __tablename__ = "content"
    __idprefix__ = "c"

    uri_part: Mapped[str] = col(Text)
    """
    A unique identifier for this piece of content. This is typically based on
    the root content title and year (of the series), and volume/issue numbers if
    present.
    """

    title: Mapped[str] = col(Text)
    """
    The title. This typically does not repeat the title given in the parent
    Content. So if a comic_series is named "My Name", the comic will be named
    "Volume 1", not "My Name Volume 1".
    """

    valid: Mapped[bool] = col(default=True)
    """When a file is detected but metadata extraction fails, this is set to
    false. For example, a `My Comic/Ch.1.cbz` that isn't actually a zip."""

    file_uri: Mapped[str] = col(Text)
    """The URI referring to the file or folder on disk, e.g.
    `file:///path/to/file.cbz`. This could extended to other protocols in the
    future, for example S3 or webdav."""

    cover_uri: Mapped[str | None] = col(Text)
    """The URI referring to the cover image for this content, if any. Same
    format as file_uri. It may transparently treat a zip file as a folder
    once. For example `file:///path/to/file.cbz/cover.png`"""

    type: Mapped[ContentType] = col(Text)
    order: Mapped[int | None] = col()
    order_parts: Mapped[list[float] | None] = col(ARRAY(REAL))
    metadata_: Mapped[dict[str, Any] | None] = col("metadata", JSONB, server_default="{}")
    file_modified_at: Mapped[datetime.datetime | None] = col(TIMESTAMP)

    parent_id: Mapped[str | None] = col(Text, ForeignKey("content.id"))
    parent: Mapped["Content | None"] = relationship(
        back_populates="children", remote_side="Content.id"
    )
    children: Mapped[list["Content"]] = relationship(back_populates="parent")

    datasource_id: Mapped[str] = col(Text, ForeignKey("data_sources.id"))
    datasource: Mapped["DataSource"] = relationship()
