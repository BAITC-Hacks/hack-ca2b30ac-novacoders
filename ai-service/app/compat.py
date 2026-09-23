"""Legacy wire placeholders only; no retired provider is instantiated or called."""

from .schemas import LegacyProviderStatus, OpenAIStatus, Providers, Versions


def mock_provider_metadata() -> Providers:
    return Providers(
        retired_provider=LegacyProviderStatus(status="SKIPPED", reviewed_anomalies=0),
        openai=OpenAIStatus(status="SKIPPED", generated_explanations=0),
    )


def mock_versions(algorithm: str, schema_version: str) -> Versions:
    return Versions(algorithm=algorithm, schema_version=schema_version,
                    retired_prompt="not-used", openai_prompt="not-used")


def health_dependencies(mock: bool) -> dict[str, str]:
    return {"openai": "disabled" if mock else "not_checked", "nvidia": "disabled"}
