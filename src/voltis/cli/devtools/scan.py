import pathlib
from urllib.parse import unquote

import click
from sqlalchemy import select
import structlog

from voltis.components.scanner.loader import get_scanner
from voltis.db.models import Library, ScannerType
from voltis.services.resource_broker import ResourceBroker
from voltis.utils.misc import now_without_tz
from voltis.utils.time import LogTime

logger = structlog.stdlib.get_logger()


async def _scan(
    rb: ResourceBroker,
    directory: str | None,
    scanner_type: ScannerType | None,
    library_id: str | None,
    dry_run: bool,
    filter_paths: list[str] | None,
):
    if not library_id and not scanner_type:
        click.echo("\nError: --type is required when not using --library", err=True)
        return
    if not directory and not library_id:
        click.echo("\nError: either directory argument or --library must be provided", err=True)
        return

    # Get or create library
    if library_id:
        async with rb.get_asession() as session:
            lib = await session.scalar(select(Library).where(Library.id == library_id))
            if not lib:
                click.echo(f"Error: Library {library_id} not found", err=True)
                return
        if scanner_type and lib.type != scanner_type:
            click.echo(
                f"Warning: Overriding library type {lib.type} with specified type {scanner_type}"
            )
            lib.type = scanner_type
    else:
        assert directory
        assert scanner_type
        dry_run = True
        lib = Library(
            id=Library.make_id(),
            name="Dry-run Library",
            sources=[{"path_uri": pathlib.Path(directory).as_posix()}],
            type=scanner_type,
            scanned_at=None,
            created_at=now_without_tz(),
            updated_at=now_without_tz(),
        )

    # Create scanner and run scan
    scanner = get_scanner(lib.type, lib, rb)
    click.echo(f"Scanning library: {lib.name or lib.id}")
    for source in lib.get_sources():
        click.echo(f"  Source: {unquote(source.path_uri)}")

    async with LogTime(logger, "Library scan"):
        result = await scanner.scan(dry_run=dry_run, filter_paths=filter_paths)

    # Display results
    click.echo("")
    if result.added:
        click.echo(f"Added ({len(result.added)}):")
        for item in result.added[:20]:
            click.echo(f"  + {unquote(item.uri)}")
        if len(result.added) > 20:
            click.echo(f"  ... and {len(result.added) - 20} more")

    if result.updated:
        click.echo(f"\nUpdated ({len(result.updated)}):")
        for item in result.updated[:20]:
            click.echo(f"  ~ {unquote(item.uri)}")
        if len(result.updated) > 20:
            click.echo(f"  ... and {len(result.updated) - 20} more")

    if result.removed:
        click.echo(f"\nRemoved ({len(result.removed)}):")
        for item, _ in result.removed[:20]:
            click.echo(f"  - {unquote(item.uri)}")
        if len(result.removed) > 20:
            click.echo(f"  ... and {len(result.removed) - 20} more")

    if not result.added and not result.updated and not result.removed:
        click.echo("No changes detected.")

    click.echo("")
    click.echo(
        f"Summary: {len(result.added)} added, {len(result.updated)} updated, {len(result.removed)} removed, {len(result.unchanged)} unchanged"
    )

    if dry_run:
        click.echo("\n(dry-run mode, no changes saved)")
