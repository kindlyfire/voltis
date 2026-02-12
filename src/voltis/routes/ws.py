from fastapi import APIRouter, WebSocket, WebSocketDisconnect
from sqlalchemy import select

from voltis.db.models import Session, User

router = APIRouter()


class ConnectionManager:
    def __init__(self):
        self.active: set[WebSocket] = set()

    def connect(self, ws: WebSocket):
        self.active.add(ws)

    def disconnect(self, ws: WebSocket):
        self.active.discard(ws)

    async def broadcast(self, message: dict):
        for ws in list(self.active):
            try:
                await ws.send_json(message)
            except Exception:
                try:
                    await ws.close()
                except Exception:
                    pass
                self.active.discard(ws)


async def _authenticate(ws: WebSocket) -> User | None:
    token = ws.cookies.get("voltis_session")
    if not token:
        return None
    rb = ws.app.state.resource_broker
    async with rb.get_asession() as session:
        result = await session.execute(select(User).join(Session).where(Session.token == token))
        return result.scalar_one_or_none()


@router.websocket("")
async def websocket_endpoint(ws: WebSocket):
    user = await _authenticate(ws)
    if user is None:
        await ws.close(code=1008)
        return

    manager: ConnectionManager = ws.app.state.ws_manager
    await ws.accept()
    manager.connect(ws)
    try:
        while True:
            await ws.receive_text()
    except WebSocketDisconnect:
        pass
    finally:
        manager.disconnect(ws)
