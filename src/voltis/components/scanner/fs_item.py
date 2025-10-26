from dataclasses import dataclass
import datetime
from typing import Literal

import anyio


@dataclass(slots=True)
class FsItem:
    type: Literal["file", "directory"]
    path: anyio.Path
    children: list["FsItem"] | None
    modified_at: datetime.datetime | None = None


_MAX_DEPTH = 5


async def list_path_uri_items(path_uri: str) -> FsItem:
    """
    Walk all folders in the given path URI recursively up to depth 5, returning
    a tree structure. Currently expects a file:// URI, but later on we may add
    support for S3, WebDav or others.

    Returns:
        FsItem: The root folder with all its children.
    """

    async def _inner(path: anyio.Path, depth: int) -> FsItem | None:
        if depth > _MAX_DEPTH:
            return None

        children: list[FsItem] = []

        async for item in path.iterdir():
            if await item.is_dir():
                child_item = await _inner(item, depth + 1)
                if child_item:
                    children.append(child_item)
            elif await item.is_file():
                stat = await item.stat()
                children.append(
                    FsItem(
                        type="file",
                        path=item,
                        children=None,
                        modified_at=datetime.datetime.fromtimestamp(stat.st_mtime),
                    )
                )

        return FsItem(type="directory", path=path, children=children if children else None)

    root_path = anyio.Path.from_uri(path_uri)
    item = await _inner(root_path, depth=1)
    assert item is not None
    return item
