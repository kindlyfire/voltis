import pathlib

import anyio
import click
from sqlalchemy import select

from voltis.components.scanner.base import ContentItem
from voltis.components.scanner.loader import ScannerType, get_scanner
from voltis.db.models import DataSource
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
    "--datasource",
    type=str,
    help="DataSource ID to use (if not provided, use in-memory datasource)",
)
@click.option(
    "--save",
    is_flag=True,
    help="Save the scan results to the database",
)
def scan(
    directory: str | None,
    scanner_type: ScannerType | None,
    datasource: str | None,
    save: bool,
):
    """Perform a test scan on a folder with a given scanner."""
    rb = ResourceBroker()
    anyio.run(_scan, rb, directory, scanner_type, datasource, save)


async def _scan(
    rb: ResourceBroker,
    directory: str | None,
    scanner_type: ScannerType | None,
    datasource_id: str | None,
    save: bool,
):
    if not datasource_id:
        if save:
            click.echo("\nError: --save requires --datasource to be specified", err=True)
        elif not scanner_type:
            click.echo("\nError: --type is required when not using --datasource", err=True)
        return
    if not directory and not datasource_id:
        click.echo("\nError: either directory argument or --datasource must be provided", err=True)
        return

    # Get or create datasource
    if datasource_id:
        async with rb.get_asession() as session:
            result = await session.scalar(select(DataSource).where(DataSource.id == datasource_id))
            if not result:
                click.echo(f"Error: DataSource {datasource_id} not found", err=True)
                return
            ds = result
        if directory:
            ds.path_uri = pathlib.Path(directory).as_uri()
        if scanner_type and ds.type != scanner_type:
            click.echo(
                f"Warning: Overriding DataSource type {ds.type} with specified type {scanner_type}"
            )
            ds.type = scanner_type
    else:
        assert directory
        ds = DataSource(
            id=DataSource.make_id(),
            path_uri=pathlib.Path(directory).as_uri(),
            type=scanner_type,
            scanned_at=None,
            created_at=now_without_tz(),
            updated_at=now_without_tz(),
        )

    # Create scanner
    scanner = get_scanner(ds.type)

    # Scan the directory
    click.echo(f"Scanning directory: {ds.path_uri}")
    content_items = await scanner.scan(ds.path_uri)

    # Match items
    if datasource_id:
        async with rb.get_asession() as session:
            to_delete = await scanner.match_from_db(session, ds.id, content_items)
    else:
        to_delete = await scanner.match_from_instances(content_items, [])

    click.echo("")

    if not content_items:
        click.echo("No content found.")
    else:
        click.echo(f"Found {len(content_items)} top-level item(s):\n")
        for item in content_items:
            _print_content_tree(item, indent=0)

    if to_delete:
        click.echo(f"Items to delete: {len(to_delete)}")
        for content in to_delete:
            click.echo(f"  - {content}")

    if save:
        async with rb.get_asession() as session:
            await scanner.save(session, ds.id, content_items, to_delete)
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
