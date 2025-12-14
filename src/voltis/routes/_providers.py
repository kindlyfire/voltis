from datetime import datetime, timedelta, timezone
from typing import Annotated

from fastapi import Depends, HTTPException, Request, Response
from sqlalchemy import select

from voltis.db.models import Session, User
from voltis.services.resource_broker import ResourceBroker

SESSION_DURATION_DAYS = 30
SESSION_REFRESH_THRESHOLD_DAYS = 14
SESSION_MAX_AGE = SESSION_DURATION_DAYS * 24 * 60 * 60


def set_session_cookie(request: Request, response: Response, token: str) -> None:
    secure = request.url.scheme == "https"
    response.set_cookie(
        key="voltis_session",
        value=token,
        httponly=True,
        secure=secure,
        samesite="lax",
        max_age=SESSION_MAX_AGE,
    )


def _rb_provider(request: Request) -> "ResourceBroker":
    return request.app.state.resource_broker


RbProvider = Annotated["ResourceBroker", Depends(_rb_provider)]


async def _maybe_user_provider(request: Request, response: Response, rb: RbProvider) -> User | None:
    session_token = request.cookies.get("voltis_session")
    if not session_token:
        return None

    async with rb.get_asession() as db_session:
        result = await db_session.execute(
            select(User, Session).join(Session).where(Session.token == session_token)
        )
        row = result.one_or_none()
        if row is None:
            return None

        user, user_session = row

        # Check if session needs refresh (expiring within 14 days)
        now = datetime.now(timezone.utc)
        expires_at_aware = user_session.expires_at.replace(tzinfo=timezone.utc)
        time_until_expiry = expires_at_aware - now

        if time_until_expiry < timedelta(days=SESSION_REFRESH_THRESHOLD_DAYS):
            new_expires_at = now + timedelta(days=SESSION_DURATION_DAYS)
            user_session.expires_at = new_expires_at.replace(tzinfo=None)
            await db_session.commit()

            set_session_cookie(request, response, user_session.token)

        return user


MaybeUserProvider = Annotated[User | None, Depends(_maybe_user_provider)]


async def _user_provider(user: MaybeUserProvider) -> User:
    if user is None:
        raise HTTPException(status_code=401, detail="Not authenticated")
    return user


UserProvider = Annotated[User, Depends(_user_provider)]


async def _admin_user_provider(user: MaybeUserProvider) -> User:
    if user is None or "ADMIN" not in user.permissions:
        raise HTTPException(status_code=403)
    return user


AdminUserProvider = Annotated[User, Depends(_admin_user_provider)]
