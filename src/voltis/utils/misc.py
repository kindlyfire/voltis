import datetime


def now_without_tz():
    return datetime.datetime.now(datetime.timezone.utc).replace(tzinfo=None)


class UnsetType:
    pass


Unset = UnsetType()
