from contextlib import asynccontextmanager

from fastapi import FastAPI, Request
from fastapi.exceptions import RequestValidationError

from .http_guard import HTTPGuard, error_response
from .routes import router
from .settings import Settings
from .providers.clients import ProviderClients
from .analysis.providers import MockAnalysisProvider, OpenAIAnalysisProvider
from .analysis.service import AnalysisService
from .explanation.service import ExplanationService


def create_app(settings: Settings | None = None) -> FastAPI:
    settings = settings or Settings()

    @asynccontextmanager
    async def lifespan(app: FastAPI):
        clients = ProviderClients(settings)
        try:
            await clients.start()
            app.state.ai_clients = clients
            provider = (MockAnalysisProvider() if settings.ai_mode == "mock"
                        else OpenAIAnalysisProvider(clients))
            app.state.analysis_service = AnalysisService(provider, settings)
            app.state.explanation_service = ExplanationService(clients)
            yield
        finally:
            await clients.close()

    app = FastAPI(title="SupplyLens AI Service", version="0.3.0", lifespan=lifespan)
    app.state.settings = settings
    app.add_middleware(HTTPGuard, settings=settings)
    app.include_router(router)

    @app.exception_handler(RequestValidationError)
    async def validation_error(request: Request, exc: RequestValidationError):
        # Do not reflect payloads, exception context, or secrets back to callers.
        fields = [{"field": ".".join(str(p) for p in error["loc"]),
                   "message": error["type"]} for error in exc.errors()]
        return error_response(422, "VALIDATION_ERROR", "Невозможно выполнить расчёт", fields=fields)

    @app.exception_handler(Exception)
    async def internal_error(request: Request, exc: Exception):
        return error_response(500, "INTERNAL_ERROR", "Не удалось сформировать рекомендации")

    return app
