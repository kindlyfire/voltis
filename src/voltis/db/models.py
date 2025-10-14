import datetime
from typing import Literal

from sqlalchemy import (
    ARRAY,
    REAL,
    ForeignKey,
    Text,
)
from sqlalchemy.dialects.postgresql import TIMESTAMP
from sqlalchemy.orm import DeclarativeBase, Mapped, relationship
from sqlalchemy.orm import mapped_column as col


class _Base(DeclarativeBase):
    pass


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

    username: Mapped[str] = col(Text, unique=True)
    password_hash: Mapped[str] = col(Text)
    permissions: Mapped[list[str]] = col(ARRAY(Text), server_default="")

    sessions: Mapped[list["Session"]] = relationship(back_populates="user")


class Session(_Base):
    __tablename__ = "sessions"

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

    path: Mapped[str] = col(Text)
    scanned_at: Mapped[datetime.datetime | None] = col(TIMESTAMP)


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

    content_id: Mapped[str] = col(Text, unique=True)
    """
    A unique identifier for this piece of content. This is typically based on
    the root content title and year (of the series), and volume/issue numbers if
    present.
    """

    type: Mapped[Literal["book", "book_series", "comic", "comic_series"]] = col(Text)

    title: Mapped[str] = col(Text)
    """
    The title. This typically does not repeat the title given in the parent
    Content. So if a comic_series is named "My Name", the comic will be named
    "Volume 1", not "My Name Volume 1".
    """

    order: Mapped[int | None] = col()
    order_parts: Mapped[list[float] | None] = col(ARRAY(REAL))

    parent_id: Mapped[str | None] = col(Text, ForeignKey("content.id"))
    parent: Mapped["Content | None"] = relationship(
        back_populates="children", remote_side="Content.id"
    )
    children: Mapped[list["Content"]] = relationship(back_populates="parent")
