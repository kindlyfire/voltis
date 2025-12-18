from pathlib import Path

from fastapi import APIRouter, HTTPException
from fastapi.responses import FileResponse

router = APIRouter()
DIST_DIR = next((p for p in Path(__file__).parents if p.name == "src")).parent / "frontend" / "dist"
INDEX_PATH = DIST_DIR / "index.html"


def _serve_index() -> FileResponse:
    return FileResponse(
        INDEX_PATH,
        media_type="text/html",
        headers={"Cache-Control": "no-cache, no-store, must-revalidate"},
    )


def _serve_asset(asset_path: str) -> FileResponse:
    file_path = (DIST_DIR / asset_path).resolve()
    if not file_path.is_relative_to(DIST_DIR):
        raise HTTPException(status_code=404, detail="Asset not found")

    return FileResponse(
        file_path,
        headers={"Cache-Control": "public, max-age=31536000, immutable"},
    )


@router.get("/")
async def serve_root() -> FileResponse:
    return _serve_index()


@router.get("/{path:path}")
async def serve_spa(path: str) -> FileResponse:
    if path.startswith("assets/"):
        return _serve_asset(path)

    return _serve_index()
