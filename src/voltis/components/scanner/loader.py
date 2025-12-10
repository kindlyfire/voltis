from typing import TYPE_CHECKING

from voltis.db.models import Library, ScannerType
from voltis.services.resource_broker import ResourceBroker

if TYPE_CHECKING:
    from voltis.components.scanner.base import ScannerBase


def get_scanner(
    type: ScannerType, library: Library, rb: ResourceBroker, no_fs: bool = False
) -> "ScannerBase":
    """Factory function to get the appropriate scanner instance."""
    if type == "comics":
        from .comics_scanner import ComicScanner

        return ComicScanner(library, rb, no_fs=no_fs)
    elif type == "books":
        from .books_scanner import BookScanner

        return BookScanner(library, rb, no_fs=no_fs)

    raise ValueError(f"Unknown scanner type: {type}")
