import unittest
from position_evidence import position_before

class PositionEvidenceTests(unittest.TestCase):
    def fixture(self):
        event={'id':'redemption','date':'2025-01-01T00:00:00Z','brokerAccountId':'a','instrumentUid':'i'}
        op={**event,'id':'buy','date':'2024-01-01T00:00:00Z','type':'OPERATION_TYPE_BUY','state':'OPERATION_STATE_EXECUTED','quantityDone':'2'}
        return event,op

    def test_scope_cancelled_orders_and_exact_quantity(self):
        event,op=self.fixture()
        operations=[op,{**op,'id':'cancelled','state':'OPERATION_STATE_CANCELED'},
                    {**op,'id':'another-account','brokerAccountId':'other'},
                    {**op,'id':'another-security','instrumentUid':'other'},
                    {**op,'id':'sell','type':'OPERATION_TYPE_SELL','quantityDone':'0.1','date':'2024-02-01T00:00:00Z'}]
        result=position_before(operations,event)
        self.assertEqual(result['quantity'],'1.9')
        self.assertEqual(result['operation_ids'],['buy','sell'])
        self.assertFalse(result['history_completeness_confirmed'])

    def test_unhandled_events_and_invalid_history_remain_unresolved(self):
        event,op=self.fixture()
        for operations in [[op,op],[{**op,'quantityDone':'0'}],[{**op,'type':'OPERATION_TYPE_SELL'}],
                           [op,{**op,'id':'input','type':'OPERATION_TYPE_INPUT_SECURITIES'}],
                           [{**op,'date':event['date']}]]:
            result=position_before(operations,event)
            self.assertEqual(result['state'],'unresolved')
            self.assertIsNone(result['quantity'])
            self.assertTrue(result['issues'])
