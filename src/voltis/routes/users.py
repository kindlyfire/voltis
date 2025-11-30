import datetime

import bcrypt
from fastapi import APIRouter, HTTPException
from pydantic import BaseModel
from sqlalchemy import select

from voltis.db.models import User
from voltis.routes._providers import RbProvider, UserProvider
from voltis.utils.misc import now_without_tz

router = APIRouter()


class UserDTO(BaseModel):
    id: str
    created_at: datetime.datetime
    updated_at: datetime.datetime
    username: str
    permissions: list[str]

    @classmethod
    def from_model(cls, model: User) -> "UserDTO":
        return cls(
            id=model.id,
            created_at=model.created_at,
            updated_at=model.updated_at,
            username=model.username,
            permissions=model.permissions,
        )


class UpsertRequest(BaseModel):
    username: str
    password: str | None = None
    permissions: list[str] = []


@router.get("")
async def list_users(
    rb: RbProvider,
    _user: UserProvider,
) -> list[UserDTO]:
    async with rb.get_asession() as session:
        result = await session.scalars(select(User))
        return [UserDTO.from_model(u) for u in result.all()]


@router.get("/me")
async def get_current_user(
    user: UserProvider,
) -> UserDTO:
    return UserDTO.from_model(user)


@router.post("/{id_or_new}")
async def upsert_user(
    rb: RbProvider,
    _user: UserProvider,
    id_or_new: str,
    body: UpsertRequest,
) -> UserDTO:
    async with rb.get_asession() as session:
        if id_or_new == "new":
            if not body.password:
                raise HTTPException(status_code=400, detail="Password is required for new users")
            password_hash = bcrypt.hashpw(body.password.encode(), bcrypt.gensalt()).decode()
            user = User(
                id=User.make_id(),
                username=body.username,
                password_hash=password_hash,
                permissions=body.permissions,
                created_at=now_without_tz(),
                updated_at=now_without_tz(),
            )
            session.add(user)
        else:
            user = await session.get(User, id_or_new)
            if not user:
                raise HTTPException(status_code=404, detail="User not found")
            user.username = body.username
            user.permissions = body.permissions
            if body.password:
                user.password_hash = bcrypt.hashpw(body.password.encode(), bcrypt.gensalt()).decode()
            user.updated_at = now_without_tz()

        await session.commit()
        await session.refresh(user)
        return UserDTO.from_model(user)


@router.delete("/{user_id}")
async def delete_user(
    rb: RbProvider,
    _user: UserProvider,
    user_id: str,
) -> dict:
    async with rb.get_asession() as session:
        user = await session.get(User, user_id)
        if not user:
            raise HTTPException(status_code=404, detail="User not found")
        await session.delete(user)
        await session.commit()
        return {"success": True}
