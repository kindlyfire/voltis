import functools
import time
from collections.abc import Awaitable, Callable
from types import TracebackType
from typing import ParamSpec, Self, TypeVar

import structlog

P = ParamSpec("P")
T = TypeVar("T")


class LogTime:
    """Async context manager that logs execution time of a block."""

    def __init__(self, logger: structlog.stdlib.BoundLogger, label: str):
        self.logger = logger
        self.label = label
        self.start: float = 0

    def __enter__(self) -> Self:
        self.start = time.perf_counter()
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_val: BaseException | None,
        exc_tb: TracebackType | None,
    ) -> None:
        elapsed = time.perf_counter() - self.start
        self.logger.debug(f"{self.label} completed", elapsed=f"{elapsed:.3f}s")

    async def __aenter__(self) -> Self:
        return self.__enter__()

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc_val: BaseException | None,
        exc_tb: TracebackType | None,
    ) -> None:
        self.__exit__(exc_type, exc_val, exc_tb)


def log_time(
    logger: structlog.stdlib.BoundLogger,
) -> Callable[[Callable[P, Awaitable[T]]], Callable[P, Awaitable[T]]]:
    """Decorator that logs the execution time of an async function."""

    def decorator(func: Callable[P, Awaitable[T]]) -> Callable[P, Awaitable[T]]:
        @functools.wraps(func)
        async def wrapper(*args: P.args, **kwargs: P.kwargs) -> T:
            async with LogTime(logger, func.__name__):
                return await func(*args, **kwargs)

        return wrapper

    return decorator
