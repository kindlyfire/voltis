from voltis.db.models import Content

from .base import LibraryFile, ScannerBase


class BookScanner(ScannerBase):
    def check_file_eligible(self, file: LibraryFile) -> bool:
        raise NotImplementedError

    async def scan_file(self, file: LibraryFile, content: Content | None) -> Content:
        raise NotImplementedError
