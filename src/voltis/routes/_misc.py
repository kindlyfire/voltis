from pydantic import BaseModel
from sqlalchemy import func, select, text

from voltis.db.models import User
from voltis.services.resource_broker import ResourceBroker


class OkResponse(BaseModel):
    success: bool = True


OK_RESPONSE = OkResponse()

_first_user_flow = True


async def get_first_user_flow(rb: ResourceBroker) -> bool:
    """First user flow = always allow registration and make the user an
    admin."""
    global _first_user_flow
    if _first_user_flow:
        async with rb.get_asession() as session:
            res = await session.scalars(
                select(func.count()).select_from(User).where(text("'ADMIN' = ANY(permissions)"))
            )
            if res.one() > 0:
                _first_user_flow = False
    return _first_user_flow
