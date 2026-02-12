from importlib.metadata import PackageNotFoundError
from importlib.metadata import version as pkg_version

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel
from sqlalchemy import func, select
from starlette.datastructures import Headers
from starlette.middleware.gzip import GZipResponder, IdentityResponder
from starlette.types import ASGIApp, Receive, Scope, Send

from voltis.db.models import User
from voltis.services.resource_broker import ResourceBroker
from voltis.services.settings import settings

from .auth import router as auth_router
from .collections import router as collections_router
from .content import router as content_router
from .custom_lists import router as custom_lists_router
from .files import router as files_router
from .libraries import router as libraries_router
from .static import router as static_router
from .users import router as users_router
from voltis.utils.scan_queue import scan_queue

from .ws import ConnectionManager
from .ws import router as ws_router

try:
    APP_VERSION = pkg_version("voltis")
except PackageNotFoundError:
    APP_VERSION = "dev"


class InfoDTO(BaseModel):
    version: str
    registration_enabled: bool
    first_user_flow: bool


def create_app(rb: ResourceBroker):
    app = FastAPI()
    app.state.resource_broker = rb

    cors_origins = ["*"] if settings.CORS == "*" else [o.strip() for o in settings.CORS.split(",")]
    app.add_middleware(
        CORSMiddleware,
        allow_origins=cors_origins,
        allow_credentials=True,
        allow_methods=["*"],
        allow_headers=["*"],
    )
    app.add_middleware(GZipMiddleware)

    app.include_router(auth_router, prefix="/api/auth")
    app.include_router(content_router, prefix="/api/content")
    app.include_router(files_router, prefix="/api/files")
    app.include_router(users_router, prefix="/api/users")
    app.include_router(libraries_router, prefix="/api/libraries")
    app.include_router(collections_router, prefix="/api/collections")
    app.include_router(custom_lists_router, prefix="/api/custom-lists")
    app.include_router(ws_router, prefix="/api/ws")

    app.state.ws_manager = ConnectionManager()
    scan_queue.ws_manager = app.state.ws_manager

    add_info_route(app, rb)
    app.include_router(static_router)

    return app


class GZipMiddleware:
    def __init__(self, app: ASGIApp, minimum_size: int = 500, compresslevel: int = 9) -> None:
        self.app = app
        self.minimum_size = minimum_size
        self.compresslevel = compresslevel

    async def __call__(self, scope: Scope, receive: Receive, send: Send) -> None:
        if scope["type"] != "http":
            await self.app(scope, receive, send)
            return

        headers = Headers(scope=scope)
        if not scope["path"].startswith("/api/files/") and "gzip" in headers.get(
            "Accept-Encoding", ""
        ):
            responder = GZipResponder(self.app, self.minimum_size, compresslevel=self.compresslevel)
        else:
            responder = IdentityResponder(self.app, self.minimum_size)

        await responder(scope, receive, send)


def add_info_route(app: FastAPI, rb: ResourceBroker):
    first_user_flow = True

    @app.get("/api/info")
    async def get_info() -> InfoDTO:
        nonlocal first_user_flow
        if first_user_flow:
            async with rb.get_asession() as session:
                res = await session.scalars(
                    select(func.count())
                    .select_from(User)
                    .where(User.permissions.contains(["ADMIN"]))
                )
                if res.one() > 0:
                    first_user_flow = False

        return InfoDTO(
            version=APP_VERSION,
            registration_enabled=settings.REGISTRATION_ENABLED,
            first_user_flow=first_user_flow,
        )
