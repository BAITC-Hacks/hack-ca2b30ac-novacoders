"""Synthetic smoke. Default: offline. --live explicitly enables real provider calls."""
import argparse
import json
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))

from fastapi.testclient import TestClient
from pydantic import ValidationError
from app.main import create_app
from app.settings import Settings


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--live', action='store_true', help='Real OpenAI calls; requires environment key, token and ALLOW_EXTERNAL_AI=true')
    args = parser.parse_args()
    try:
        settings = (Settings(ai_mode='live', ai_provider_max_retries=0) if args.live else
                    Settings(ai_mode='mock', allow_external_ai=False, ai_local_mode=True, ai_service_token=''))
    except ValidationError:
        print('CONFIG_ERROR: проверьте ALLOW_EXTERNAL_AI=true, AI_SERVICE_TOKEN и настройки окружения.')
        return 1
    if args.live and not settings.openai_api_key.get_secret_value():
        print('CONFIG_ERROR: OPENAI_API_KEY отсутствует в окружении.')
        return 1
    payload = json.loads((ROOT / 'contracts/request.example.json').read_text())
    if len(payload.get('products', [])) != 1 or payload.get('is_demo') is not True:
        print('SMOKE_INPUT_ERROR: разрешён только один синтетический товар.')
        return 1
    payload.setdefault('settings', {}).update(
        anomalyReviewEnabled=True, explanationMode='ALL', maxAIExplanations=1)
    token = settings.ai_service_token.get_secret_value()
    headers = {'Authorization': 'Bearer ' + token} if token else {}
    with TestClient(create_app(settings), client=('127.0.0.1', 50000)) as client:
        response = client.post('/v1/recommendations', json=payload, headers=headers)
    if response.status_code != 200:
        print('HTTP', response.status_code, response.json().get('error', {}).get('code', 'unknown'))
        return 1
    result = response.json()
    item = result['recommendations'][0]
    assert item['recommendedQuantity'] == 90, 'Unexpected synthetic calculation'
    print('quantity=90; arithmetic=150+20-50-30; is_demo=true')
    print(next(w['message'] for w in item['warnings'] if w['code'] == 'RESULT_SOURCES'))
    print('analysis_status=' + item['anomalyAnalysis']['status'])
    print('explanation=' + item['explanation']['generatedBy'])
    if args.live and (item['anomalyAnalysis']['status'] == 'FAILED' or item['explanation']['generatedBy'] != 'OPENAI'):
        print('Live AI smoke incomplete; deterministic/template fallback preserved the calculation.')
        return 2
    return 0


if __name__ == '__main__':
    try:
        exit_code = main()
    except Exception:
        # Never print SDK/configuration exceptions or input bodies in a manual smoke.
        print('SMOKE_ERROR: проверка не завершена; проверьте конфигурацию и доступность сервиса.')
        exit_code = 1
    raise SystemExit(exit_code)
