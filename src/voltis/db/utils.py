from typing import Any, Generic, TypeVar

from pydantic import BaseModel
from sqlalchemy import event
from sqlalchemy.orm import Mapped


T = TypeVar("T", bound=BaseModel)


class JSONMetadataMixin(Generic[T]):
    metadata_: Mapped[dict[str, Any] | None]
    _metadata_obj: T | None = None
    _metadata_snapshot: dict[str, Any] | None = None
    _metadata_class: type[T]

    @property
    def metadata_obj(self) -> T:
        if self._metadata_obj is None:
            self._metadata_obj = self._metadata_class.model_validate(self.metadata_ or {})
            self._metadata_snapshot = self.metadata_
        return self._metadata_obj

    def _sync_metadata(self) -> None:
        if self._metadata_obj is not None:
            dumped = self._metadata_obj.model_dump(mode="json", exclude_defaults=True)
            if dumped != self._metadata_snapshot:
                self.metadata_ = dumped


@event.listens_for(JSONMetadataMixin, "before_insert", propagate=True)
@event.listens_for(JSONMetadataMixin, "before_update", propagate=True)
def sync_metadata_before_save(mapper, connection, target):
    target._sync_metadata()
