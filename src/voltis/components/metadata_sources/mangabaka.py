from typing import Any, Literal, NotRequired, TypedDict

import httpx
import structlog
from tenacity import retry, retry_if_exception, stop_after_attempt, wait_exponential

logger = structlog.stdlib.get_logger()

type SeriesType = Literal["manga", "novel", "manhwa", "manhua", "oel", "other"]
type SeriesStatus = Literal["cancelled", "completed", "hiatus", "releasing", "unknown", "upcoming"]
type ContentRating = Literal["safe", "suggestive", "erotica", "pornographic"]
type SeriesState = Literal["active", "merged", "deleted"]


class CoverRaw(TypedDict):
    url: str | None
    size: NotRequired[int | None]
    height: NotRequired[int | None]
    width: NotRequired[int | None]
    blurhash: NotRequired[str | None]
    thumbhash: NotRequired[str | None]
    format: NotRequired[str | None]


class CoverScaled(TypedDict):
    x1: str | None
    x2: str | None
    x3: str | None


class Cover(TypedDict):
    raw: CoverRaw
    x150: CoverScaled
    x250: CoverScaled
    x350: CoverScaled


class Publisher(TypedDict):
    name: str | None
    type: str | None
    note: str | None


class AnimeInfo(TypedDict):
    start: str | None
    end: str | None


class SecondaryTitle(TypedDict):
    type: Literal["alternative", "native", "official", "unofficial"]
    title: str
    note: NotRequired[str | None]


class TagV2(TypedDict):
    id: int
    parent_id: int | None
    name: str
    name_path: str
    description: NotRequired[str | None]
    is_spoiler: NotRequired[bool]
    content_rating: NotRequired[ContentRating]
    series_count: NotRequired[int]
    level: NotRequired[int]


class SourceEntry(TypedDict, total=False):
    id: int | str | None
    rating: float | None
    rating_normalized: float | None


class Relationships(TypedDict, total=False):
    main_story: list[int]
    adaptation: list[int]
    prequel: list[int]
    sequel: list[int]
    side_story: list[int]
    spin_off: list[int]
    alternative: list[int]
    other: list[int]


class Series(TypedDict, total=False):
    id: int
    state: SeriesState
    merged_with: int | None
    title: str
    native_title: str | None
    romanized_title: str | None
    secondary_titles: dict[str, list[SecondaryTitle]] | None
    cover: Cover
    authors: list[str] | None
    artists: list[str] | None
    description: str | None
    year: int | None
    status: SeriesStatus
    is_licensed: bool
    has_anime: bool
    anime: AnimeInfo | None
    content_rating: ContentRating
    type: SeriesType
    rating: float | None
    final_volume: str | None
    total_chapters: str | None
    links: list[str] | None
    publishers: list[Publisher] | None
    genres: list[str]
    genres_v2: list[TagV2] | None
    tags: list[str] | None
    tags_v2: list[TagV2] | None
    last_updated_at: str | None
    relationships: Relationships | None
    source: dict[str, SourceEntry]


class Pagination(TypedDict):
    count: int
    page: int
    limit: int
    next: str | None
    previous: str | None


class SeriesGetResponse(TypedDict):
    status: int
    data: Series


class SeriesSearchResponse(TypedDict):
    status: int
    pagination: Pagination
    data: list[Series]


# -- Client --

_BASE_URL = "https://api.mangabaka.dev"

_RETRYABLE_STATUS = frozenset({429, 500, 502, 503, 504})


def _is_retryable(exc: BaseException) -> bool:
    if isinstance(exc, httpx.TimeoutException):
        return True
    if isinstance(exc, httpx.HTTPStatusError):
        return exc.response.status_code in _RETRYABLE_STATUS
    return False


class MangaBaka:
    def __init__(self, *, base_url: str = _BASE_URL, timeout: float = 15) -> None:
        self._client = httpx.AsyncClient(
            base_url=base_url,
            timeout=timeout,
            headers={"Accept": "application/json"},
        )

    async def close(self) -> None:
        await self._client.aclose()

    async def __aenter__(self) -> MangaBaka:
        return self

    async def __aexit__(self, *_: object) -> None:
        await self.close()

    @retry(
        retry=retry_if_exception(_is_retryable),
        stop=stop_after_attempt(3),
        wait=wait_exponential(multiplier=1, min=1, max=10),
        reraise=True,
    )
    async def series_get(self, id: int) -> Series:
        resp = await self._client.get(f"/v1/series/{id}")
        resp.raise_for_status()
        body: SeriesGetResponse = resp.json()
        series = body["data"]
        if series.get("state") == "merged" and "merged_with" in series and series["merged_with"]:
            return await self.series_get(series["merged_with"])
        return series

    @retry(
        retry=retry_if_exception(_is_retryable),
        stop=stop_after_attempt(3),
        wait=wait_exponential(multiplier=1, min=1, max=10),
        reraise=True,
    )
    async def series_search(
        self,
        *,
        q: str | None = None,
        type: list[SeriesType] | None = None,
    ) -> SeriesSearchResponse:
        params: dict[str, Any] = {}
        if q is not None:
            params["q"] = q
        if type is not None:
            params["type"] = type

        resp = await self._client.get("/v1/series/search", params=params)
        resp.raise_for_status()
        body: SeriesSearchResponse = resp.json()
        return body
