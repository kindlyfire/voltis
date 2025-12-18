from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from voltis.services.resource_broker import ResourceBroker

from .auth import router as auth_router
from .collections import router as collections_router
from .content import router as content_router
from .files import router as files_router
from .libraries import router as libraries_router
from .static import router as static_router
from .users import router as users_router


def create_app(rb: ResourceBroker):
    app = FastAPI()
    app.state.resource_broker = rb

    app.add_middleware(
        CORSMiddleware,
        allow_origins=["http://localhost:5173", "http://127.0.0.1:5173"],
        allow_credentials=True,
        allow_methods=["*"],
        allow_headers=["*"],
    )

    app.include_router(auth_router, prefix="/api/auth")
    app.include_router(content_router, prefix="/api/content")
    app.include_router(files_router, prefix="/api/files")
    app.include_router(users_router, prefix="/api/users")
    app.include_router(libraries_router, prefix="/api/libraries")
    app.include_router(collections_router, prefix="/api/collections")
    app.include_router(static_router)

    return app
