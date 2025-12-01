import pathlib

import anyio
import click
from sqlalchemy import select

from voltis.components.scanner.base import ContentItem
from voltis.components.scanner.loader import ScannerType, get_scanner
from voltis.db.models import Library
from voltis.services.resource_broker import ResourceBroker
from voltis.utils.misc import now_without_tz


@click.command()
@click.argument(
    "directory", type=click.Path(exists=True, file_okay=False, dir_okay=True), required=False
)
@click.option(
    "-t",
    "--type",
    "scanner_type",
    type=click.Choice(["comics"]),
    help="Type of scanner to use",
)
@click.option(
    "--library",
    type=str,
    help="Library ID to use (if not provided, use in-memory library)",
)
@click.option(
    "--save",
    is_flag=True,
    help="Save the scan results to the database",
)
def scan(
    directory: str | None,
    scanner_type: ScannerType | None,
    library: str | None,
    save: bool,
):
    """Perform a test scan on a folder with a given scanner."""
    rb = ResourceBroker()
    anyio.run(_scan, rb, directory, scanner_type, library, save)


async def _scan(
    rb: ResourceBroker,
    directory: str | None,
    scanner_type: ScannerType | None,
    library_id: str | None,
    save: bool,
):
    if not library_id:
        if save:
            click.echo("\nError: --save requires --library to be specified", err=True)
            return
        elif not scanner_type:
            click.echo("\nError: --type is required when not using --library", err=True)
            return
    if not directory and not library_id:
        click.echo("\nError: either directory argument or --library must be provided", err=True)
        return

    # Get or create library
    if library_id:
        async with rb.get_asession() as session:
            result = await session.scalar(select(Library).where(Library.id == library_id))
            if not result:
                click.echo(f"Error: Library {library_id} not found", err=True)
                return
            lib = result
        if scanner_type and lib.type != scanner_type:
            click.echo(
                f"Warning: Overriding Library type {lib.type} with specified type {scanner_type}"
            )
            lib.type = scanner_type
    else:
        assert directory
        lib = Library(
            id=Library.make_id(),
            sources=[{"path_uri": pathlib.Path(directory).as_uri()}],
            type=scanner_type,
            scanned_at=None,
            created_at=now_without_tz(),
            updated_at=now_without_tz(),
        )

    # Create scanner
    scanner = get_scanner(lib.type)
    path_uris = [source.path_uri for source in lib.get_sources()]

    # Scan all paths and merge results
    all_content_items: list[ContentItem] = []
    for path_uri in path_uris:
        # TODO: If the same item is present in more than one folder, it should
        # be rejected. Maybe even reject both to be sure.
        click.echo(f"Scanning: {path_uri}")
        content_items = await scanner.scan(path_uri)
        all_content_items.extend(content_items)

    # Match items
    if library_id:
        async with rb.get_asession() as session:
            to_delete = await scanner.match_from_db(session, lib.id, all_content_items)
    else:
        to_delete = await scanner.match_from_instances(all_content_items, [])

    click.echo("")

    to_not_delete = [
        content
        for content in ContentItem.flatten(all_content_items)
        if content.content_inst not in to_delete
    ]
    for i, item in enumerate(to_not_delete):
        click.echo(f"Updating metadata for {item.title} ({i + 1}/{len(to_not_delete)})")
        assert item.content_inst
        await scanner.scan_item(item.content_inst)

    if not all_content_items:
        click.echo("No content found.")
    else:
        click.echo(f"Found {len(all_content_items)} top-level item(s):\n")
        for item in all_content_items:
            _print_content_tree(item, indent=0)

    if to_delete:
        click.echo(f"Items to delete: {len(to_delete)}")
        for content in to_delete:
            click.echo(f"  - {content}")

    if save:
        async with rb.get_asession() as session:
            await scanner.save(session, lib.id, all_content_items, to_delete)
            await session.commit()
        click.echo("\nScan results saved to database.")


def _print_content_tree(item: ContentItem, indent: int = 0):
    """Recursively print a ContentItem tree."""
    prefix = "    " * indent

    if item.content_inst:
        click.echo(f"{prefix}{item.content_inst}")
    else:
        click.echo(f"{prefix}[No Content] {item.title} ({item.uri_part})")

    if item.children:
        for child in item.children:
            _print_content_tree(child, indent + 1)
