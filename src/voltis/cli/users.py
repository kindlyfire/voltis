import sys

import bcrypt
import click
from sqlalchemy import select
from sqlalchemy.exc import IntegrityError

from ..db.models import User
from ..services.resource_broker import ResourceBroker
from ..utils.misc import now_without_tz


def _read_password(password: str) -> str:
    """Read password from argument or stdin if '-'."""
    if password == "-":
        return sys.stdin.read().strip()
    if len(password) < 8:
        click.echo("Error: Password must be at least 8 characters long", err=True)
        sys.exit(1)
    return password


async def _create(rb: ResourceBroker, username: str, password: str, admin: bool) -> None:
    password = _read_password(password)
    password_hash = bcrypt.hashpw(password.encode(), bcrypt.gensalt()).decode()
    permissions = ["ADMIN"] if admin else []

    async with rb.get_asession() as session:
        user = User(
            id=User.make_id(),
            username=username,
            password_hash=password_hash,
            permissions=permissions,
            created_at=now_without_tz(),
            updated_at=now_without_tz(),
        )
        session.add(user)
        try:
            await session.commit()
        except IntegrityError:
            click.echo(f"Error: User '{username}' already exists", err=True)
            sys.exit(1)
        click.echo(f"Created user '{username}' with id {user.id}")


async def _update(
    rb: ResourceBroker,
    name: str,
    username: str | None,
    password: str | None,
    admin: bool | None,
) -> None:
    async with rb.get_asession() as session:
        result = await session.execute(select(User).where(User.username == name))
        user = result.scalar_one_or_none()
        if not user:
            click.echo(f"User '{name}' not found", err=True)
            sys.exit(1)

        if username:
            user.username = username
        if password:
            password = _read_password(password)
            user.password_hash = bcrypt.hashpw(password.encode(), bcrypt.gensalt()).decode()
        if admin is True:
            if "ADMIN" not in user.permissions:
                user.permissions = [*user.permissions, "ADMIN"]
        elif admin is False:
            user.permissions = [p for p in user.permissions if p != "ADMIN"]

        user.updated_at = now_without_tz()
        await session.commit()
        click.echo(f"Updated user '{name}'")
