"""Whole HTTP pipeline with the real SDK parser and fake HTTP transport only."""
import asyncio
from copy import deepcopy
import json

import httpx
import pytest
from fastapi.testclient import TestClient

from app.main import create_app
from app.schemas import RecommendationResponse
from app.settings import Settings


AUTH = {'Authorization': 'Bearer test-only-token'}
ANALYSIS = {'proposals': [], 'no_anomalies_reason': 'В переданном контексте замечаний не найдено.'}
EXPLANATION = {'sentences': [{'text': 'Свободный остаток и своевременные поставки уменьшают потребность в закупке.',
                            'reason_ids': ['stock_deducted', 'transit_deducted']}]}


def settings(**kwargs):
    return Settings(ai_mode='live', allow_external_ai=True, ai_service_token='test-only-token',
                    openai_api_key='test-only-not-a-key', ai_provider_max_retries=0,
                    openai_analysis_model='analysis-test', openai_explanation_model='explanation-test', **kwargs)


def envelope(value):
    return {'id':'resp_test','object':'response','created_at':1,'model':'test-model',
        'status':'completed','error':None,'incomplete_details':None,
        'parallel_tool_calls':False,'tools':[],
        'output':[{'id':'msg_test','type':'message','role':'assistant','status':'completed',
                   'content':[{'type':'output_text','text':json.dumps(value,ensure_ascii=False),'annotations':[]}]}]}


def transport(monkeypatch, analysis=ANALYSIS, explanation=EXPLANATION, fail=None):
    calls=[]
    async def send(self, request, **kwargs):
        body=json.loads(request.content)
        calls.append((self,body))
        assert str(request.url)=='https://api.openai.com/v1/responses'
        assert body['store'] is False and not body.get('tools')
        task='analysis' if body['model']=='analysis-test' else 'explanation'
        if fail==task:
            return httpx.Response(403,json={'error':{'message':'private-provider-error','type':'permission_error'}},request=request)
        return httpx.Response(200,json=envelope(analysis if task=='analysis' else explanation),request=request)
    monkeypatch.setattr(httpx.AsyncClient,'send',send)
    return calls


def post(payload,config=None):
    before=deepcopy(payload)
    with TestClient(create_app(config or settings()),client=('127.0.0.1',50000)) as client:
        response=client.post('/v1/recommendations',json=payload,headers=AUTH)
    assert response.status_code==200,response.text
    assert payload==before
    return RecommendationResponse.model_validate(response.json())


def test_success_and_explanation_failure_same_arithmetic(monkeypatch,mvp_payload,caplog):
    calls=transport(monkeypatch)
    success=post(mvp_payload)
    assert len(calls)==2 and calls[0][0] is calls[1][0]  # Same lifecycle HTTP client.
    assert [c[1]['model'] for c in calls]==['analysis-test','explanation-test']
    item=success.recommendations[0]
    assert item.recommended_quantity==90 and item.forecast==150
    assert item.anomaly_analysis.status=='COMPLETED' and item.anomaly_analysis.source=='live'
    assert item.explanation.generated_by=='OPENAI' and success.source=='live'
    assert success.providers.openai.generated_explanations==1
    context=json.loads(calls[1][1]['input'][0]['content'])
    assert set(context)=={'calculation','confirmed_reasons','warnings','assumptions_not_facts','missing_fields'}
    assert 'analytical_history' not in context['calculation']
    assert 'proposals' not in json.dumps(context)
    assert 'stock_deducted' in context['confirmed_reasons']
    calls=transport(monkeypatch,fail='explanation')
    failure=post(mvp_payload)
    other=failure.recommendations[0]
    assert len(calls)==2 and failure.source=='fallback'
    assert other.calculation==item.calculation and other.recommended_quantity==90
    assert other.explanation.generated_by=='SYSTEM'
    assert other.explanation.short==item.explanation.short
    assert other.explanation.details[:4]==item.explanation.details[:4]
    assert 'EXPLANATION_FALLBACK' in {w.code for w in other.warnings}
    assert failure.providers.openai.status=='FALLBACK'
    assert 'private-provider-error' not in caplog.text
    assert 'test-only-not-a-key' not in caplog.text
    assert mvp_payload['products'][0]['name'] not in caplog.text


@pytest.mark.parametrize('bad',[
    {'sentences':[{'text':'Закажите 999 единиц.','reason_ids':['coverage_demand']}]},
    {'sentences':[{'text':'Закупка связана с новым клиентом.','reason_ids':['invented_customer']}]},
    {'sentences':[]},
    {'sentences':[{'text':'Первая фраза. Вторая фраза.','reason_ids':['coverage_demand']}]},
])
def test_invalid_explanation_uses_template(monkeypatch,mvp_payload,bad):
    transport(monkeypatch,explanation=bad)
    item=post(mvp_payload).recommendations[0]
    assert item.recommended_quantity==90
    assert item.explanation.generated_by=='SYSTEM'
    assert 'EXPLANATION_FALLBACK' in {w.code for w in item.warnings}


def test_analysis_access_error_is_not_no_anomalies(monkeypatch,mvp_payload):
    calls=transport(monkeypatch,fail='analysis')
    result=post(mvp_payload)
    item=result.recommendations[0]
    assert item.recommended_quantity==90 and item.anomaly_analysis.status=='FAILED'
    assert item.anomaly_analysis.error_code=='access_denied'
    assert item.requires_manual_review and item.action=='REVIEW'
    assert item.explanation.generated_by=='OPENAI' and len(calls)==2
    assert 'ANALYSIS_FALLBACK' in {w.code for w in item.warnings}
    assert any('deterministic' in text for text in item.explanation.details)


def proposal(row='month:2026-06'):
    return {'proposals':[{'evidence_row_ids':[row], 'anomaly_type':'possible_one_off_order',
        'proposed_action':'consider_excluding_from_regular_demand',
        'explanation':'Возможно разовое событие.','missing_evidence':['Нет подтверждения клиента.'],
        'referenced_client_ids':[],'referenced_dates':[]}],'no_anomalies_reason':None}


@pytest.mark.parametrize('row,expected',[('month:2026-06','PENDING_REVIEW'),('invented','FAILED')])
def test_proposals_never_authorize_sales_edits(monkeypatch,mvp_payload,row,expected):
    calls=transport(monkeypatch,analysis=proposal(row))
    item=post(mvp_payload).recommendations[0]
    assert item.anomaly_analysis.status==expected
    assert item.recommended_quantity==90 and item.adjustments==[]
    assert item.calculation.analytical_history[0].original_quantity==150
    assert 'Возможно разовое событие' not in calls[1][1]['input'][0]['content']


def test_existing_business_rule_is_applied_after_analysis(monkeypatch,mvp_payload):
    transport(monkeypatch,analysis=proposal('month:2026-08'))
    mvp_payload['monthlySales'][-1]['quantity']=255
    mvp_payload['transactions']=[
        {'code1C':'000317_','transactionId':'regular','date':'2026-08-10','quantity':155},
        {'code1C':'000317_','transactionId':'oneoff','date':'2026-08-11','quantity':100,
         'confirmedOneOff':True,'businessReason':'Подтверждено бизнесом'}]
    item=post(mvp_payload).recommendations[0]
    assert item.recommended_quantity==90 and item.calculation.anomaly_excluded_quantity==100
    assert item.calculation.analytical_history[-1].original_quantity==255


def test_small_batch_is_sequential_and_contexts_isolated(monkeypatch,mvp_payload):
    calls=transport(monkeypatch)
    for field in ['products','monthlySales','inventory','incomingShipments']:
        original=deepcopy(mvp_payload[field])
        mvp_payload[field].extend(dict(r,code1C='000318_') for r in original)
    result=post(mvp_payload)
    assert [c[1]['model'] for c in calls]==['analysis-test','explanation-test']*2
    contexts=[json.loads(c[1]['input'][0]['content']) for c in calls[::2]]
    assert [c['sku'] for c in contexts]==['000317_','000318_']
    assert len({id(c[0]) for c in calls})==1
    assert [i.recommended_quantity for i in result.recommendations]==[90,90]


@pytest.mark.parametrize('mode,limit', [('NONE',50),('ALL',0)])
def test_existing_explanation_controls(monkeypatch,mvp_payload,mode,limit):
    calls=transport(monkeypatch)
    mvp_payload['settings'].update(explanationMode=mode,maxAIExplanations=limit)
    item=post(mvp_payload).recommendations[0]
    assert len(calls)==1 and item.explanation.generated_by=='SYSTEM'


def test_timeout_preserves_calculation(monkeypatch,mvp_payload):
    async def send(self, request, **kwargs):
        await asyncio.sleep(1)
        raise AssertionError('timeout should cancel the await')
    monkeypatch.setattr(httpx.AsyncClient,'send',send)
    item=post(mvp_payload,settings(ai_provider_timeout_seconds=0.01)).recommendations[0]
    assert item.recommended_quantity==90 and item.anomaly_analysis.error_code=='timeout'
    assert item.explanation.generated_by=='SYSTEM'


def test_request_budget_skips_later_calls(monkeypatch,mvp_payload):
    calls=transport(monkeypatch)
    item=post(mvp_payload,settings(ai_request_timeout_seconds=0.1)).recommendations[0]
    assert calls==[] and item.recommended_quantity==90
    assert item.anomaly_analysis.error_code=='timeout'


def test_manual_smoke_live_has_no_retries(monkeypatch, capsys):
    from scripts import smoke_pipeline
    monkeypatch.setattr('sys.argv', ['smoke_pipeline.py', '--live'])
    monkeypatch.setenv('ALLOW_EXTERNAL_AI', 'true')
    monkeypatch.setenv('AI_SERVICE_TOKEN', 'test-only-token')
    monkeypatch.setenv('OPENAI_API_KEY', 'test-only-not-a-key')
    monkeypatch.setenv('AI_PROVIDER_MAX_RETRIES', '3')
    calls = []
    async def send(self, request, **kwargs):
        calls.append(json.loads(request.content))
        return httpx.Response(429, json={'error': {'message': 'secret-provider-body', 'type': 'rate_limit_error'}}, request=request)
    monkeypatch.setattr(httpx.AsyncClient, 'send', send)
    assert smoke_pipeline.main() == 2
    assert len(calls) == 2  # One analysis and one explanation, zero SDK retries even on 429.
    output = capsys.readouterr().out
    assert 'secret-provider-body' not in output and 'test-only-not-a-key' not in output


@pytest.mark.parametrize('permission,key', [('false', 'test-only-not-a-key'), ('true', '')])
def test_smoke_requires_permission_and_environment_key(monkeypatch, capsys, permission, key):
    from scripts import smoke_pipeline
    monkeypatch.setattr('sys.argv', ['smoke_pipeline.py', '--live'])
    monkeypatch.setenv('ALLOW_EXTERNAL_AI', permission)
    monkeypatch.setenv('AI_SERVICE_TOKEN', 'test-only-token')
    monkeypatch.setenv('OPENAI_API_KEY', key)
    assert smoke_pipeline.main() == 1
    assert 'CONFIG_ERROR' in capsys.readouterr().out
