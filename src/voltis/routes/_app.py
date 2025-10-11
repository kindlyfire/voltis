from fastapi import FastAPI

from voltis.services.resource_broker import ResourceBroker

from .auth import router as auth_router
from .collections import router as collections_router
from .libraries import router as libraries_router
from .users import router as users_router


def create_app(rb: ResourceBroker):
    app = FastAPI()
    app.state.resource_broker = rb

    app.include_router(auth_router, prefix="/auth")
    app.include_router(users_router, prefix="/users")
    app.include_router(libraries_router, prefix="/libraries")
    app.include_router(collections_router, prefix="/collections")

    return app
