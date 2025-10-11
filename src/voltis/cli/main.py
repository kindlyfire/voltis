import click

from .migrate import migrate
from .run import run


@click.group()
def main(): ...


main.add_command(run)
main.add_command(migrate)

if __name__ == "__main__":
    main()
