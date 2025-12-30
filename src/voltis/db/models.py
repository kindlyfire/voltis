import datetime
import random
import string
from typing import Any, Literal, TypedDict

from colorama import Fore, Style
from pydantic import BaseModel
from sqlalchemy import (
    ARRAY,
    REAL,
    Boolean,
    ForeignKey,
    Integer,
    Text,
    inspect,
)
from sqlalchemy.dialects.postgresql import JSONB, TIMESTAMP
from sqlalchemy.orm import DeclarativeBase, Mapped, relationship
from sqlalchemy.orm import mapped_column as col

from voltis.utils.misc import now_without_tz

ContentType = Literal["comic", "comic_series", "book", "book_series"]
ScannerType = Literal["comics", "books"]
GroupingContentTypes: list[ContentType] = ["book_series", "comic_series"]
LeafContentTypes: list[ContentType] = ["book", "comic"]


class _Base(DeclarativeBase):
    def as_dict(self):
        return {c.key: getattr(self, c.key) for c in inspect(self).mapper.column_attrs}

    def __repr__(self):
        d = self.as_dict()
        parts = [f"{k}={Fore.YELLOW}{v!r}{Style.RESET_ALL}" for k, v in d.items()]
        d_str = ", ".join(parts)
        return f"<{Fore.CYAN}{self.__class__.__name__}{Style.RESET_ALL} {d_str}>"

    @classmethod
    def make_id(cls) -> str:
        if not hasattr(cls, "__idprefix__"):
            raise NotImplementedError("gen_id requires __idprefix__ to be set")
        rand = "".join(random.choices(string.ascii_letters + string.digits, k=8))
        return f"{getattr(cls, '__idprefix__')}_{rand}"


class _DefaultColumns:
    id: Mapped[str] = col(Text, primary_key=True)
    created_at: Mapped[datetime.datetime] = col(TIMESTAMP, server_default="")
    updated_at: Mapped[datetime.datetime] = col(
        TIMESTAMP,
        server_default="",
        onupdate=now_without_tz,
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

    token: Mapped[str] = col(Text, primary_key=True)
    user_id: Mapped[str] = col(Text, ForeignKey("users.id"))
    expires_at: Mapped[datetime.datetime] = col(TIMESTAMP)

    user: Mapped["User"] = relationship(back_populates="sessions")


class LibrarySource(BaseModel):
    path_uri: str


class Library(_Base, _DefaultColumns):
    """
    A library is a collection of books, comics, series or movies. It defines
    scanning rules for one or more folders, and items are grouped under its
    banner.

    Right now, only folders are supported, but I would like to add support for
    S3. And maybe native rclone support.
    """

    __tablename__ = "libraries"
    __idprefix__ = "l"

    name: Mapped[str] = col(Text)
    type: Mapped[ScannerType] = col(Text)
    scanned_at: Mapped[datetime.datetime | None] = col(TIMESTAMP)
    sources: Mapped[list[Any]] = col("sources", JSONB, server_default="{}")

    contents: Mapped[list["Content"]] = relationship(back_populates="library")

    def get_sources(self) -> list[LibrarySource]:
        return [LibrarySource.model_validate(source) for source in self.sources]

    def set_sources(self, sources: list[LibrarySource]):
        self.sources = [source.model_dump(mode="json") for source in sources]


class ContentMetadata(TypedDict, total=False):
    pages: list[tuple[str, int, int]]
    """Names, including extension, of the page files of a comic, with the image
    width and height."""

    authors: list[str]
    description: str
    publisher: str
    language: str
    publication_date: str


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

    uri: Mapped[str] = col(Text, unique=True)
    """The full URI, including parents, separated by a `/`. Example: My Comic/v0_ch1"""

    title: Mapped[str] = col(Text)
    """
    The title. This typically does not repeat the title given in the parent
    Content. So if a comic_series is named "My Name", the comic will be named
    "Volume 1", not "My Name Volume 1".
    """

    valid: Mapped[bool] = col(default=True)
    """When a file is detected but metadata extraction fails, this is set to
    false. For example, a `My Comic/Ch.1.cbz` that isn't actually a zip."""

    file_uri: Mapped[str | None] = col(Text)
    """The URI referring to the file or folder on disk, e.g.
    `file:///path/to/file.cbz`. This could extended to other protocols in the
    future, for example S3 or webdav."""
    file_mtime: Mapped[datetime.datetime | None] = col(TIMESTAMP)
    file_size: Mapped[int | None] = col(Integer)

    cover_uri: Mapped[str | None] = col(Text)
    """The URI referring to the cover image for this content, if any. Same
    format as file_uri. It may transparently treat a zip file as a folder
    once. For example `file:///path/to/file.cbz/cover.png`"""

    type: Mapped[ContentType] = col(Text)
    order: Mapped[int | None] = col()
    order_parts: Mapped[list[float]] = col(ARRAY(REAL))
    meta: Mapped[ContentMetadata] = col("meta", JSONB, server_default="{}")

    parent_id: Mapped[str | None] = col(Text, ForeignKey("content.id"))
    parent: Mapped["Content | None"] = relationship(
        back_populates="children", remote_side="Content.id"
    )
    children: Mapped[list["Content"]] = relationship(back_populates="parent")

    library_id: Mapped[str] = col(Text, ForeignKey("libraries.id"))
    library: Mapped["Library"] = relationship()

    def mutate_meta(self) -> ContentMetadata:
        meta = self.meta = self.meta or {}
        return meta


ReadingStatus = Literal["reading", "completed", "on_hold", "dropped", "plan_to_read"]
CustomListVisibility = Literal["public", "private", "unlisted"]


class ReadingProgress(TypedDict, total=False):
    current_page: int
    """Used by the paged comics reader."""
    progress_percent: float
    """Not currently used. Maybe by the ebook reader in the future."""


class UserToContent(_Base):
    __tablename__ = "user_to_content"
    __idprefix__ = "utc"

    id: Mapped[str] = col(Text, primary_key=True)
    user_id: Mapped[str] = col(Text, ForeignKey("users.id"))
    library_id: Mapped[str | None] = col(Text, ForeignKey("libraries.id"))
    uri: Mapped[str] = col(Text)

    starred: Mapped[bool] = col(Boolean, default=False, server_default="false")
    status: Mapped[ReadingStatus | None] = col(Text)
    status_updated_at: Mapped[datetime.datetime | None] = col(TIMESTAMP)
    notes: Mapped[str | None] = col(Text)
    rating: Mapped[int | None] = col(Integer)
    progress: Mapped[ReadingProgress] = col("progress", JSONB, server_default="{}")
    progress_updated_at: Mapped[datetime.datetime | None] = col(TIMESTAMP)

    user: Mapped["User"] = relationship()
    library: Mapped["Library | None"] = relationship()


class CustomList(_Base, _DefaultColumns):
    __tablename__ = "custom_lists"
    __idprefix__ = "cl"

    name: Mapped[str] = col(Text)
    description: Mapped[str | None] = col(Text)
    visibility: Mapped[CustomListVisibility] = col(Text)
    user_id: Mapped[str] = col(Text, ForeignKey("users.id"))

    user: Mapped["User"] = relationship()
    entries: Mapped[list["CustomListToContent"]] = relationship(back_populates="custom_list")


class CustomListToContent(_Base, _DefaultColumns):
    __tablename__ = "custom_list_to_content"
    __idprefix__ = "clc"

    custom_list_id: Mapped[str] = col(Text, ForeignKey("custom_lists.id"))
    library_id: Mapped[str] = col(Text, ForeignKey("libraries.id"))
    uri: Mapped[str] = col(Text)
    notes: Mapped[str | None] = col(Text)
    order: Mapped[int | None] = col(Integer)

    custom_list: Mapped["CustomList"] = relationship(back_populates="entries")
