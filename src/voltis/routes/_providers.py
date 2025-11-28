from typing import Annotated

from fastapi import Depends, HTTPException, Request
from sqlalchemy import select

from voltis.db.models import Session, User
from voltis.services.resource_broker import ResourceBroker


def _rb_provider(request: Request) -> "ResourceBroker":
    return request.app.state.resource_broker


RbProvider = Annotated["ResourceBroker", Depends(_rb_provider)]


async def _maybe_user_provider(request: Request, rb: RbProvider) -> User | None:
    session_token = request.cookies.get("voltis_session")
    if not session_token:
        return None

    async with rb.get_asession() as session:
        result = await session.execute(
            select(User).join(Session).where(Session.token == session_token)
        )
        return result.scalar_one_or_none()


MaybeUserProvider = Annotated[User | None, Depends(_maybe_user_provider)]


async def _user_provider(user: MaybeUserProvider) -> User:
    if user is None:
        raise HTTPException(status_code=401, detail="Not authenticated")
    return user


UserProvider = Annotated[User, Depends(_user_provider)]
