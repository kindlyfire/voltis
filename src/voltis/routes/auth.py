from datetime import datetime, timedelta, timezone
from typing import Annotated
from uuid import uuid4

import bcrypt
from fastapi import APIRouter, Body, HTTPException, Request, Response
from sqlalchemy import delete, select
from sqlalchemy.exc import IntegrityError

from voltis.db.models import Session, User
from voltis.routes._misc import OK_RESPONSE, OkResponse
from voltis.routes._providers import (
    SESSION_DURATION_DAYS,
    RbProvider,
    set_session_cookie,
)
from voltis.services.settings import settings

router = APIRouter()


@router.post("/login")
async def route_auth_login(
    rb: RbProvider,
    request: Request,
    response: Response,
    username: Annotated[str, Body()],
    password: Annotated[str, Body()],
) -> OkResponse:
    async with rb.get_asession() as session:
        # Find user
        result = await session.execute(select(User).where(User.username == username))
        user = result.scalar_one_or_none()
        if not user:
            raise HTTPException(status_code=401, detail="Invalid credentials")

    if not bcrypt.checkpw(password.encode(), user.password_hash.encode()):
        raise HTTPException(status_code=401, detail="Invalid credentials")

    async with rb.get_asession() as session:
        expires_at = datetime.now(timezone.utc) + timedelta(days=SESSION_DURATION_DAYS)
        user_session = Session(
            token=uuid4().hex + uuid4().hex,
            user_id=user.id,
            expires_at=expires_at.replace(tzinfo=None),
        )
        session.add(user_session)
        await session.commit()

    set_session_cookie(request, response, user_session.token)

    return OK_RESPONSE


@router.post("/register")
async def route_auth_register(
    rb: RbProvider,
    request: Request,
    response: Response,
    username: Annotated[str, Body(min_length=2)],
    password: Annotated[str, Body(min_length=8)],
) -> OkResponse:
    if settings.REGISTRATION_ENABLED is False:
        raise HTTPException(status_code=403, detail="Registration is disabled")

    password_hash = bcrypt.hashpw(password.encode(), bcrypt.gensalt()).decode()

    async with rb.get_asession() as session:
        user = User(id=User.make_id(), username=username, password_hash=password_hash)
        expires_at = datetime.now(timezone.utc) + timedelta(days=SESSION_DURATION_DAYS)
        user_session = Session(
            token=uuid4().hex + uuid4().hex,
            user_id=user.id,
            expires_at=expires_at.replace(tzinfo=None),
        )
        session.add_all([user, user_session])

        try:
            await session.commit()
        except IntegrityError:
            raise HTTPException(status_code=400, detail="Username already exists")

    set_session_cookie(request, response, user_session.token)

    return OK_RESPONSE


@router.post("/logout")
async def route_auth_logout(
    rb: RbProvider,
    request: Request,
    response: Response,
) -> OkResponse:
    session_token = request.cookies.get("voltis_session")
    if not session_token:
        raise HTTPException(status_code=401, detail="Not authenticated")

    async with rb.get_asession() as session:
        await session.execute(delete(Session).where(Session.token == session_token))
        await session.commit()

    response.delete_cookie(key="voltis_session")

    return OK_RESPONSE
