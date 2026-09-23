from copy import deepcopy

import pytest
from fastapi.testclient import TestClient

from app.main import create_app
from app.schemas import RecommendationRequest, RecommendationResponse


def request(payload, settings):
    before = deepcopy(payload)
    with TestClient(create_app(settings), client=("127.0.0.1", 50000)) as client:
        response = client.post('/v1/recommendations', json=payload)
    assert response.status_code == 200, response.text
    assert payload == before
    return RecommendationResponse.model_validate(response.json()).recommendations[0]


@pytest.mark.parametrize('transit,expected', [(30,90),(70,50)])
def test_acceptance_through_endpoint(mvp_payload, local_settings, transit, expected):
    mvp_payload['incomingShipments'][0]['quantity'] = transit
    item = request(mvp_payload, local_settings)
    c = item.calculation
    assert (c.forecast_demand,c.safety_stock,c.available_stock,c.eligible_in_transit) == (150,20,50,transit)
    assert item.recommended_quantity == expected
    assert item.calculation_source == 'python' and item.source == 'fallback'
    assert item.is_demo and item.anomaly_analysis.is_demo
    assert item.explanation.generated_by == 'SYSTEM'
    assert f'= {expected:g}.' in item.explanation.details[1]


@pytest.mark.parametrize('stock,minimum,multiple,expected', [(47,20,1,93),(47,20,20,100),(200,20,20,0),(139,20,1,20)])
def test_minimum_multiple_and_zero(mvp_payload,local_settings,stock,minimum,multiple,expected):
    mvp_payload['inventory'][0]['freeStock']=stock
    mvp_payload['products'][0].update(minimumOrderQuantity=minimum,orderMultiple=multiple)
    assert request(mvp_payload,local_settings).recommended_quantity == expected


def test_fractional_unit_step(mvp_payload,local_settings):
    mvp_payload['products'][0].update(unit='кг',quantityStep=0.25,orderMultiple=0.1,minimumOrderQuantity=0)
    mvp_payload['inventory'][0]['freeStock']=139.4
    assert request(mvp_payload,local_settings).recommended_quantity == 1


@pytest.mark.parametrize('missing', ['stock','history','minimum','multiple','unit_step','stock_date','transit_quantity'])
def test_critical_unknown_returns_null(mvp_payload,local_settings,missing):
    if missing=='stock': mvp_payload['inventory']=[]
    if missing=='history': mvp_payload['monthlySales']=[]
    if missing=='minimum': mvp_payload['products'][0]['minimumOrderQuantity']=None
    if missing=='multiple': mvp_payload['products'][0]['orderMultiple']=None
    if missing=='unit_step': mvp_payload['products'][0].update(unit='кг',quantityStep=None)
    if missing=='stock_date': mvp_payload['inventory'][0]['stockAsOfDate']='2026-08-31'
    if missing=='transit_quantity': mvp_payload['incomingShipments'][0]['quantity']=None
    item=request(mvp_payload,local_settings)
    assert item.recommended_quantity is None and item.missing_fields
    assert item.processing_status=='needs_review' and item.action=='REVIEW'


@pytest.mark.parametrize('eta', [None,'2026-09-22','2026-08-31'])
def test_only_timely_shipments_deducted(mvp_payload,local_settings,eta):
    mvp_payload['incomingShipments'][0]['expectedDate']=eta
    item=request(mvp_payload,local_settings)
    assert item.calculation.eligible_in_transit==0 and item.recommended_quantity==120
    assert item.requires_manual_review


def test_aggregate_transit_is_not_assumed_timely(mvp_payload,local_settings):
    mvp_payload.pop('incomingShipments')
    item=request(mvp_payload,local_settings)
    assert item.recommended_quantity==120
    assert 'TRANSIT_DATE_UNKNOWN' in {w.code for w in item.warnings}


def test_sales_change_forecast(mvp_payload,local_settings):
    for row in mvp_payload['monthlySales']: row['quantity']*=2
    item=request(mvp_payload,local_settings)
    assert item.forecast==300 and item.calculation.safety_stock==40
    assert item.recommended_quantity==260


def test_growth_once_and_seasonality_normalized(mvp_payload,local_settings):
    for row in mvp_payload['monthlySales']: row['quantity']*=2
    for factor in mvp_payload['seasonality']: factor['coefficient']=2
    mvp_payload['products'][0]['additionalGrowthRate']=0.1
    item=request(mvp_payload,local_settings)
    assert item.forecast==330  # underlying 5/day * seasonal 2 * growth 1.1 * 30
    assert item.calculation.trend_factor==1


def test_missing_seasonality_warns(mvp_payload,local_settings):
    mvp_payload['seasonality']=None
    item=request(mvp_payload,local_settings)
    assert item.forecast==150
    assert 'SEASONALITY_MISSING' in {w.code for w in item.warnings}


def test_unfinished_month_is_ignored(mvp_payload,local_settings):
    mvp_payload['monthlySales'].append({'code1C':'000317_','month':'2026-09','quantity':99999})
    assert request(mvp_payload,local_settings).forecast==150


def test_month_end_zero_does_not_create_stockout(mvp_payload,local_settings):
    mvp_payload['monthlyStock']=[{'code1C':'000317_','month':'2026-08','quantity':0}]
    item=request(mvp_payload,local_settings)
    assert item.forecast==150 and item.adjustments==[]


def test_confirmed_stockout_compensation(mvp_payload,local_settings):
    mvp_payload['monthlySales'][-1]['quantity']=105
    mvp_payload['availability']=[{'code1C':'000317_','startDate':'2026-08-01','endDate':'2026-08-31','stockoutDays':10}]
    item=request(mvp_payload,local_settings)
    assert item.calculation.stockout_compensation==50 and item.forecast==150
    history=item.calculation.analytical_history[-1]
    assert history.original_quantity==105 and history.estimated_regular_demand==155
    assert len(item.adjustments)==1


@pytest.mark.parametrize('confirmed', [False,True])
def test_only_confirmed_order_excluded(mvp_payload,local_settings,confirmed):
    mvp_payload['monthlySales'][-1]['quantity']=255
    mvp_payload['transactions']=[
        {'code1C':'000317_','transactionId':'regular','date':'2026-08-10','quantity':155},
        {'code1C':'000317_','transactionId':'oneoff','date':'2026-08-11','quantity':100,
         'confirmedOneOff':confirmed,'businessReason':'Подтверждено менеджером'},
    ]
    item=request(mvp_payload,local_settings)
    assert item.calculation.analytical_history[-1].original_quantity==255
    if confirmed:
        assert item.forecast==150 and item.calculation.anomaly_excluded_quantity==100
        assert item.adjustments[0].row_id=='oneoff'
    else:
        assert item.forecast>150 and item.adjustments==[]
    assert all(p.status=='needs_review' for p in item.anomaly_analysis.proposals)


def test_unreconciled_oneoff_not_applied(mvp_payload,local_settings):
    mvp_payload['transactions']=[{'code1C':'000317_','transactionId':'oneoff','date':'2026-08-11',
        'quantity':100,'confirmedOneOff':True,'businessReason':'Подтверждено менеджером'}]
    item=request(mvp_payload,local_settings)
    assert item.recommended_quantity is None and item.adjustments==[]


def test_sparse_daily_history_needs_completeness_flag(mvp_payload,local_settings):
    mvp_payload['monthlySales']=[]
    mvp_payload['salesHistory']=[{'code1C':'000317_','rowId':str(i),'date':date,'quantity':q}
        for i,(date,q) in enumerate([('2026-06-15',150),('2026-07-15',155),('2026-08-15',155)])]
    assert request(mvp_payload,local_settings).recommended_quantity is None
    mvp_payload['settings']['salesHistoryComplete']=True
    assert request(mvp_payload,local_settings).recommended_quantity==90


def test_separate_composite_keys(mvp_payload,local_settings):
    second=deepcopy(mvp_payload['products'][0]);second['warehouseId']='second'
    mvp_payload['products'].append(second)
    for row in mvp_payload['monthlySales']:
        row.update(warehouseId='warehouse-demo',supplierId='supplier-demo')
    mvp_payload['monthlySales'] += [dict(r,warehouseId='second',quantity=r['quantity']*2) for r in mvp_payload['monthlySales']]
    mvp_payload['inventory'].append(dict(mvp_payload['inventory'][0],warehouseId='second'))
    with TestClient(create_app(local_settings),client=('127.0.0.1',50000)) as client:
        response=client.post('/v1/recommendations',json=mvp_payload)
    assert response.status_code==200
    items=response.json()['recommendations']
    assert [r['forecast'] for r in items]==[150,300]
    assert [r['recommendedQuantity'] for r in items]==[90,290]


@pytest.mark.parametrize('field', ['stock', 'transit'])
def test_more_stock_or_timely_transit_never_increases_order(mvp_payload, local_settings, field):
    quantities = []
    for value in (0, 30, 50, 70, 150, 200):
        if field == 'stock':
            mvp_payload['inventory'][0]['freeStock'] = value
        else:
            mvp_payload['incomingShipments'][0]['quantity'] = value
        quantities.append(request(mvp_payload, local_settings).recommended_quantity)
    assert quantities == sorted(quantities, reverse=True)


def test_unknown_stock_is_not_zero(mvp_payload, local_settings):
    mvp_payload['inventory'][0].update(freeStock=None, totalStock=None, reservedStock=None)
    item = request(mvp_payload, local_settings)
    assert item.calculation.available_stock is None
    assert item.recommended_quantity is None
    assert 'inventory.freeStock' in item.missing_fields


def test_unconfirmed_availability_is_not_stockout(mvp_payload, local_settings):
    mvp_payload['availability'] = [{'code1C': '000317_', 'startDate': '2026-08-01',
                                   'endDate': '2026-08-31', 'available': None, 'stockoutDays': None}]
    item = request(mvp_payload, local_settings)
    assert item.calculation.stockout_compensation == 0
    assert item.adjustments == [] and item.recommended_quantity == 90
