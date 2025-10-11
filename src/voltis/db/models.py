import datetime

from sqlalchemy import (
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
    created_at: Mapped[datetime.datetime] = col(TIMESTAMP, nullable=False, server_default="NOW()")


class User(_Base, _DefaultColumns):
    __tablename__ = "users"

    username: Mapped[str] = col(Text, nullable=False, unique=True)
    password_hash: Mapped[str] = col(Text, nullable=False)

    sessions: Mapped[list["Session"]] = relationship(back_populates="user")


class Session(_Base):
    __tablename__ = "sessions"

    token: Mapped[str] = col(Text, primary_key=True)
    user_id: Mapped[str] = col(Text, ForeignKey("users.id"), nullable=False)

    user: Mapped["User"] = relationship(back_populates="sessions")
