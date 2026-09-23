from typing import Literal

from pydantic import Field, SecretStr, field_validator, model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    # Deliberately never auto-load .env or log settings.
    model_config = SettingsConfigDict(env_file=None, extra="ignore", hide_input_in_errors=True)

    ai_mode: Literal["mock", "live"] = "mock"
    allow_external_ai: bool = False
    ai_local_mode: bool = False
    ai_service_token: SecretStr = Field(default=SecretStr(""), repr=False)
    openai_api_key: SecretStr = Field(default=SecretStr(""), repr=False)
    openai_model: str = "gpt-6-luna"
    openai_analysis_model: str = ""
    openai_explanation_model: str = ""
    ai_provider_max_retries: int = Field(default=1, ge=0, le=3)
    ai_max_output_tokens: int = Field(default=2048, gt=0, le=16384)
    ai_analysis_max_rows: int = Field(default=120, gt=0, le=1000)
    ai_analysis_max_context_bytes: int = Field(default=65536, gt=0, le=1048576)
    ai_request_timeout_seconds: float = Field(default=30, gt=0, le=600)
    ai_provider_timeout_seconds: float = Field(default=15, gt=0, le=300)
    ai_max_request_bytes: int = Field(default=50 * 1024 * 1024, gt=0)
    ai_max_products: int = Field(default=500, gt=0, le=10000)
    ai_max_concurrent_api_calls: int = Field(default=2, gt=0, le=100)

    @field_validator("openai_model", "openai_analysis_model", "openai_explanation_model")
    @classmethod
    def normalize_model(cls, value):
        return value.strip()

    def model_for(self, task: Literal["analysis", "explanation"]) -> str:
        if task == "analysis":
            return self.openai_analysis_model or self.openai_model
        if task == "explanation":
            return self.openai_explanation_model or self.openai_model
        raise ValueError("Unknown AI task")

    @model_validator(mode="after")
    def secure_mode(self):
        if not self.openai_model:
            raise ValueError("OPENAI_MODEL must not be empty")
        token = self.ai_service_token.get_secret_value()
        if token and (token != token.strip() or not token.isascii()):
            raise ValueError("AI_SERVICE_TOKEN must be an ASCII token without outer whitespace")
        if not token and not (self.ai_mode == "mock" and self.ai_local_mode):
            raise ValueError("AI_SERVICE_TOKEN required outside explicit local mock mode")
        if self.ai_mode == "live" and not self.allow_external_ai:
            raise ValueError("Live mode requires ALLOW_EXTERNAL_AI=true")
        return self
