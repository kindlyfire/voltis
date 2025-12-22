import datetime


def now_without_tz():
    return datetime.datetime.now(datetime.timezone.utc).replace(tzinfo=None)


class UnsetType:
    pass


Unset = UnsetType()


def notnone[T](value: T | None) -> T:
    """Assert that a value is not None and return it with a non-None type."""
    if value is None:
        raise ValueError("Expected value to be not None")
    return value
