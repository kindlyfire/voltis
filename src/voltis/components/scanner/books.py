from voltis.db.models import Content

from .base import FoundItem, ScannerBase


class BookScanner(ScannerBase):
    async def scan_items(self, items: list[FoundItem]) -> list[Content]:
        """
        We walk through folders and find all .epub files. We read the metadata
        from the file to group them by series and keep the right order, if
        possible.
        """

        raise NotImplementedError()
