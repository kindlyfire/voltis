from typing import Final
import dotenv
from pydantic_settings import BaseSettings

dotenv.load_dotenv()


class Settings(BaseSettings):
    model_config = {"env_prefix": "APP_"}

    DB_URL: str = ""


settings: Final[Settings] = Settings()
