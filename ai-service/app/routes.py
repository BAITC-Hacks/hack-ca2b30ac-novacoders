from fastapi import APIRouter, Request

from .pipeline import run_pipeline, LiveConfigurationError
from .http_guard import error_response
from .schemas import RecommendationRequest, RecommendationResponse
from .compat import health_dependencies

router = APIRouter()


@router.get("/health")
async def health(request: Request):
    settings = request.app.state.settings
    return {
        "status": "ok", "service": "recommendation-ai-service", "version": "0.3.0", "pythonCalculationAvailable": True,
        "mode": settings.ai_mode, "livePipelineAvailable": settings.ai_mode == "live" and bool(settings.openai_api_key.get_secret_value()),
        "dependencies": health_dependencies(settings.ai_mode == "mock"),
    }


@router.post("/v1/recommendations", response_model=RecommendationResponse)
async def recommendations(payload: RecommendationRequest, request: Request):
    try:
        return await run_pipeline(payload, request.app.state.settings,
                                  request.app.state.analysis_service,
                                  request.app.state.explanation_service)
    except LiveConfigurationError:
        return error_response(503, "AI_NOT_CONFIGURED", "Live требует настроенного OpenAI-клиента и ключа",
                              request_id=str(payload.request_id))
