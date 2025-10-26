from typing import TYPE_CHECKING, Literal

if TYPE_CHECKING:
    from voltis.components.scanner.base import ScannerBase

ScannerType = Literal["comics", "books"]


def get_scanner(type: ScannerType) -> "ScannerBase":
    """Factory function to get the appropriate scanner instance."""
    if type == "comics":
        from .comics_scanner import ComicScanner

        return ComicScanner()
    elif type == "books":
        from .books_scanner import BookScanner

        return BookScanner()

    raise ValueError(f"Unknown scanner type: {type}")
