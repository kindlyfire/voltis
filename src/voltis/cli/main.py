import click

from .devtools.scan import scan
from .migrate import migrate
from .run import run


@click.group()
def main(): ...


@click.group()
def devtools():
    """Development/testing tools."""


main.add_command(run)
main.add_command(migrate)
main.add_command(devtools)

devtools.add_command(scan)


if __name__ == "__main__":
    main()
