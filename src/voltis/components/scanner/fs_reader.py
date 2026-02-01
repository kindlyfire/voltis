import asyncio
import datetime
import stat
from dataclasses import dataclass
from typing import Callable

import structlog
from anyio import CapacityLimiter, Path, create_task_group

from voltis.db.models import LibrarySource
from voltis.utils.time import log_time

logger = structlog.stdlib.get_logger()


class LibrarySourceMissing(Exception):
    pass


@dataclass(slots=True)
class LibraryFile:
    path: str
    mtime: datetime.datetime | None = None
    size: int | None = None

    def has_changed(self, other: LibraryFile) -> bool:
        return self.mtime != other.mtime or self.size != other.size


@log_time(logger)
async def get_fs_items(
    sources: list[LibrarySource], eligible_cb: Callable[[LibraryFile], bool]
) -> list[LibraryFile]:
    items = await asyncio.gather(
        *[_get_fs_items_source(source) for source in sources],
    )
    return [item for sublist in items for item in sublist if eligible_cb(item)]


@log_time(logger)
async def _get_fs_items_source(source: LibrarySource):
    path = Path(source.path_uri)
    limiter = CapacityLimiter(20)
    files: list[LibraryFile] = []

    async def get_file_info(item: Path) -> None:
        async with limiter:
            stat_ = await item.stat()
            if not stat.S_ISREG(stat_.st_mode):
                return
            mtime = datetime.datetime.fromtimestamp(stat_.st_mtime)
            files.append(LibraryFile(path=item.as_posix(), mtime=mtime, size=stat_.st_size))

    if not await path.is_dir():
        raise LibrarySourceMissing(f"Source path does not exist: {path}")

    async with create_task_group() as tg:
        async for item in path.glob("**/*"):
            tg.start_soon(get_file_info, item)

    return files
