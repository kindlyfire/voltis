from typing import TYPE_CHECKING

from voltis.db.models import Library
from voltis.services.resource_broker import ResourceBroker

if TYPE_CHECKING:
    from .base import Scanner


def get_scanner(
    rb: ResourceBroker,
    library: Library,
    dry_run: bool = False,
    filter_paths: list[str] | None = None,
    force: bool = False,
    events: bool = True,
) -> "Scanner":
    """Factory function to get the appropriate scanner instance."""
    if library.type == "comics":
        from .comics_scanner import ComicsScanner

        return ComicsScanner(
            dry_run=dry_run,
            rb=rb,
            library=library,
            filter_paths=filter_paths,
            force=force,
            events=events,
        )
    elif library.type == "books":
        from .books_scanner import BooksScanner

        return BooksScanner(
            dry_run=dry_run,
            rb=rb,
            library=library,
            filter_paths=filter_paths,
            force=force,
            events=events,
        )

    raise ValueError(f"Unknown scanner type: {type}")
