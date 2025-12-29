from pydantic import BaseModel


class OkResponse(BaseModel):
    success: bool = True


OK_RESPONSE = OkResponse()
