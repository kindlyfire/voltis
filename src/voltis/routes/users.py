import datetime

import bcrypt
from fastapi import APIRouter, HTTPException
from pydantic import BaseModel
from sqlalchemy import select

from voltis.db.models import User
from voltis.routes._misc import OK_RESPONSE, OkResponse
from voltis.routes._providers import AdminUserProvider, RbProvider, UserProvider
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
    _user: AdminUserProvider,
) -> list[UserDTO]:
    async with rb.get_asession() as session:
        result = await session.scalars(select(User))
        return [UserDTO.from_model(u) for u in result.all()]


@router.get("/me")
async def get_current_user(
    user: UserProvider,
) -> UserDTO:
    return UserDTO.from_model(user)


@router.post("/me")
async def update_current_user(
    rb: RbProvider,
    user: UserProvider,
    body: UpsertRequest,
) -> UserDTO:
    async with rb.get_asession() as session:
        session.add(user)
        user.username = body.username
        if body.password:
            user.password_hash = bcrypt.hashpw(body.password.encode(), bcrypt.gensalt()).decode()
        user.updated_at = now_without_tz()
        await session.commit()
        await session.refresh(user)
        return UserDTO.from_model(user)


@router.post("/{id_or_new}")
async def upsert_user(
    rb: RbProvider,
    user: AdminUserProvider,
    id_or_new: str,
    body: UpsertRequest,
) -> UserDTO:
    if user.id == id_or_new and "ADMIN" not in body.permissions:
        raise HTTPException(status_code=403, detail="Cannot remove admin permission from yourself")

    async with rb.get_asession() as session:
        if id_or_new == "new":
            if not body.password:
                raise HTTPException(status_code=400, detail="Password is required for new users")
            password_hash = bcrypt.hashpw(body.password.encode(), bcrypt.gensalt()).decode()
            user_ = User(
                id=User.make_id(),
                username=body.username,
                password_hash=password_hash,
                permissions=body.permissions,
                created_at=now_without_tz(),
                updated_at=now_without_tz(),
            )
            session.add(user_)
        else:
            user_ = await session.get(User, id_or_new)
            if not user_:
                raise HTTPException(status_code=404, detail="User not found")
            user_.username = body.username
            user_.permissions = body.permissions
            if body.password:
                user_.password_hash = bcrypt.hashpw(
                    body.password.encode(), bcrypt.gensalt()
                ).decode()
            user_.updated_at = now_without_tz()

        await session.commit()
        await session.refresh(user_)
        return UserDTO.from_model(user_)


@router.delete("/{user_id}")
async def delete_user(
    rb: RbProvider,
    _user: UserProvider,
    user_id: str,
) -> OkResponse:
    async with rb.get_asession() as session:
        user = await session.get(User, user_id)
        if not user:
            raise HTTPException(status_code=404, detail="User not found")
        await session.delete(user)
        await session.commit()
        return OK_RESPONSE
