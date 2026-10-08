import os

from pydantic import BaseModel

DATABASE_URL = os.environ["RECS_DATABASE_URL"]  # doc: CFG-RECS_DATABASE_URL
MODEL_PATH = os.getenv("RECS_MODEL_PATH", "/var/lib/recs/e5")  # doc: CFG-RECS_MODEL_PATH


class RecsResponse(BaseModel):  # doc: DTO-recs-response
    skus: list[str]
    fallback: bool


def for_user():  # doc: API-recs.for-user
    ...
