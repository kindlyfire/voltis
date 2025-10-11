from typing import Annotated
from uuid import uuid4

import bcrypt
from fastapi import APIRouter, Body, HTTPException, Request, Response
from sqlalchemy import select
from sqlalchemy.exc import IntegrityError

from voltis.db.models import Session, User
from voltis.services.resource_broker import RbProvider

router = APIRouter()


@router.post("/login")
async def route_auth_login(
    rb: RbProvider,
    request: Request,
    response: Response,
    username: Annotated[str, Body()],
    password: Annotated[str, Body()],
):
    async with rb.get_asession() as session:
        # Find user
        result = await session.execute(select(User).where(User.username == username))
        user = result.scalar_one_or_none()
        if not user:
            raise HTTPException(status_code=401, detail="Invalid credentials")

    # Check password
    if not bcrypt.checkpw(password.encode(), user.password_hash.encode()):
        raise HTTPException(status_code=401, detail="Invalid credentials")

    async with rb.get_asession() as session:
        # Store session
        user_session = Session(token=uuid4().hex + uuid4().hex, user_id=user.id)
        session.add(user_session)
        await session.commit()

    # Set cookie
    secure = request.url.scheme == "https"
    response.set_cookie(
        key="voltis_session",
        value=user_session.token,
        httponly=True,
        secure=secure,
        samesite="lax",
    )

    return {"success": True}


@router.post("/register")
async def route_auth_register(
    rb: RbProvider,
    request: Request,
    response: Response,
    username: Annotated[str, Body()],
    password: Annotated[str, Body()],
):
    async with rb.get_asession() as session:
        # Check if user exists
        result = await session.execute(select(User).where(User.username == username))
        existing_user = result.scalar_one_or_none()
        if existing_user:
            raise HTTPException(status_code=400, detail="Username already exists")

    # Hash password
    password_hash = bcrypt.hashpw(password.encode(), bcrypt.gensalt()).decode()

    async with rb.get_asession() as session:
        # Create user
        user = User(id=uuid4().hex, username=username, password_hash=password_hash)
        session.add(user)
        # Create session
        user_session = Session(token=uuid4().hex + uuid4().hex, user_id=user.id)
        session.add(user_session)

        try:
            await session.commit()
        except IntegrityError:
            raise HTTPException(status_code=400, detail="Username already exists")

    # Set cookie
    secure = request.url.scheme == "https"
    response.set_cookie(
        key="voltis_session",
        value=user_session.token,
        httponly=True,
        secure=secure,
        samesite="lax",
    )

    return {"success": True}
