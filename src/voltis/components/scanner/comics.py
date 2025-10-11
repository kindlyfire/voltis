from .base import ScannerBase


class ComicScanner(ScannerBase):
    async def scan_items(self, item):
        """
        We scan walk through folders. If a folder contains a .cbz or .cbr file,
        we consider it a comic. Sub-folders are ignored for now.

        We create one Content for the series, and then one child Content for
        each volume/chapter.
        """

        raise NotImplementedError()
