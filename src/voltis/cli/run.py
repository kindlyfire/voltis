import logging
import uvicorn
import click


@click.command()
def run():
    from ..routes._app import create_app

    app = create_app()
    uvicorn.run(app, host="127.0.0.1", port=8000, log_level=logging.INFO)
