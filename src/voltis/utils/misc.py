import datetime
from enum import Enum
from typing import Literal

from pydantic import BaseModel


def now_without_tz():
    return datetime.datetime.now(datetime.timezone.utc).replace(tzinfo=None)


class _Unset(Enum):
    token = object()


Unset = _Unset.token
UnsetType = Literal[_Unset.token]


def notnone[T](value: T | None) -> T:
    """Assert that a value is not None and return it with a non-None type."""
    if value is None:
        raise ValueError("Expected value to be not None")
    return value


class PaginatedResponse[T](BaseModel):
    data: list[T]
    total: int
