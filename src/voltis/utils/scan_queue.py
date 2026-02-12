from __future__ import annotations

import asyncio
from dataclasses import asdict
from typing import TYPE_CHECKING

import structlog
from anyio import create_task_group

from voltis.components.scanner.base import ScannerEventProgress, ScannerEventUpdateSummary
from voltis.components.scanner.loader import get_scanner
from voltis.db.models import Library
from voltis.services.resource_broker import ResourceBroker

if TYPE_CHECKING:
    from voltis.components.scanner.base import Scanner
    from voltis.routes.ws import ConnectionManager

logger = structlog.stdlib.get_logger()


class ScanQueue:
    def __init__(self):
        self.queue: list[Scanner] = []
        self.ws_manager: ConnectionManager | None = None
        self._running = False
        self._current_summary: ScannerEventUpdateSummary | None = None
        self._current_progress: ScannerEventProgress | None = None

    async def enqueue(self, rb: ResourceBroker, library_id: str, force: bool = False):
        if any(s.library.id == library_id for s in self.queue):
            logger.info("Scan already queued", library_id=library_id)
            return

        async with rb.get_asession() as session:
            library = await session.get(Library, library_id)
            if library is None:
                raise ValueError(f"Library {library_id} not found")

        scanner = get_scanner(rb, library, force=force)
        self.queue.append(scanner)
        logger.info("Scan enqueued", library_id=library_id, queue_size=len(self.queue))

        if not self._running:
            self._running = True
            asyncio.create_task(self._process_queue())

    async def _process_queue(self):
        try:
            async with create_task_group() as tg:
                tg.start_soon(self._broadcast_loop)
                await self._run_scans()
                tg.cancel_scope.cancel()
            await self._broadcast()
        finally:
            self._running = False

    async def _run_scans(self):
        while self.queue:
            scanner = self.queue[0]
            lib = scanner.library
            self._current_summary = None
            self._current_progress = None

            logger.info("Scan starting", library_id=lib.id, library_name=lib.name)
            try:
                async with create_task_group() as tg:
                    tg.start_soon(self._consume_events, scanner)
                    result = await scanner.scan()
                logger.info(
                    "Scan finished",
                    library_id=lib.id,
                    added=len(result.added),
                    updated=len(result.updated),
                    removed=len(result.removed),
                )
            except Exception:
                logger.exception("Scan failed", library_id=lib.id)
            finally:
                self.queue.pop(0)

    async def _consume_events(self, scanner: Scanner):
        async with scanner.events_recv:
            async for event in scanner.events_recv:
                if isinstance(event, ScannerEventUpdateSummary):
                    self._current_summary = event
                    await self._broadcast()
                elif isinstance(event, ScannerEventProgress):
                    self._current_progress = event
                    if event.processed == event.total:
                        await self._broadcast()

    async def _broadcast_loop(self):
        while self._running:
            await asyncio.sleep(1)
            await self._broadcast()

    async def _broadcast(self):
        if not self.ws_manager:
            return
        await self.ws_manager.broadcast(self._build_status())

    def _build_status(self) -> dict:
        items = []
        for i, scanner in enumerate(self.queue):
            entry: dict = {
                "library_id": scanner.library.id,
                "library_name": scanner.library.name,
            }
            if i == 0 and self._running:
                entry["status"] = "running"
                if self._current_summary:
                    entry["summary"] = asdict(self._current_summary)
                if self._current_progress:
                    entry["progress"] = asdict(self._current_progress)
            else:
                entry["status"] = "queued"
            items.append(entry)
        return {"type": "scan_status", "queue": items}


scan_queue = ScanQueue()
