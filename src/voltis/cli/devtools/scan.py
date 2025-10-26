import pathlib
import anyio
import click
from sqlalchemy import select

from voltis.components.scanner.base import ContentItem
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
    required=True,
    help="Type of scanner to use",
)
@click.option(
    "--datasource",
    type=str,
    help="DataSource ID to use (if not provided, use in-memory datasource)",
)
@click.pass_obj
def scan(rb: ResourceBroker, directory: str | None, scanner_type: str, datasource: str | None):
    """Perform a test scan on a folder with a given scanner."""
    anyio.run(_scan, rb, directory, scanner_type, datasource)


async def _scan(
    rb: ResourceBroker, directory: str | None, scanner_type: str, datasource_id: str | None
):
    """Async implementation of the scan command."""

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
    else:
        if not directory:
            click.echo(
                "Error: directory argument is required when --datasource is not provided", err=True
            )
            return
        ds = DataSource(
            id=DataSource.make_id(),
            path_uri=pathlib.Path(directory).as_uri(),
            type=scanner_type,
            scanned_at=None,
            created_at=now_without_tz(),
            updated_at=now_without_tz(),
        )

    # Create scanner
    if scanner_type == "comics":
        from voltis.components.scanner.comics import ComicScanner

        scanner = ComicScanner()
    else:
        click.echo(f"Error: Unknown scanner type {scanner_type}", err=True)
        return

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
