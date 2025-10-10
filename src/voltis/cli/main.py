import click

from .run import run


@click.group()
def main(): ...


main.add_command(run)
