import copy
import unittest
from adjusted_trade_audit import candidate, audit
from test_financial_evidence import cash


def fixture():
    row = {'id':'source:1','record':1,
           'raw':{'Event':'BUY','Date':'2024-02-06 14:05:09','Currency':'RUB',
                  'Symbol':'NEW','FeeCurrency':'','DoNotAdjustCash':'False'},
           'decimals':{'Quantity':'2.5','Price':'40','FeeTax':'1.25','NKD':'0'}}
    op = {'id':'op','brokerAccountId':'a','date':'2024-02-06T11:05:09.123Z',
          'type':'OPERATION_TYPE_BUY','state':'OPERATION_STATE_EXECUTED',
          'instrumentType':'share','instrumentUid':'uid','ticker':'OLD','quantityDone':'1',
          'price':cash('100'),'payment':cash('-100'),'commission':cash('-1.25')}
    return row, op


class AdjustedTradeTests(unittest.TestCase):
    def test_observed_ratio_not_permission_to_post(self):
        row, op = fixture()
        result = candidate(row, op)
        self.assertEqual(result['observed_quantity_ratio'], '2.5')
        self.assertEqual(result['cash'], '100')
        matches = {'rows':[{'source_record':1,'status':'unmatched'}],
                   'broker_operations':{'a:op':{'raw':op}}}
        result = audit({'audit':{'sha256':'source'},'rows':[row]},matches)
        self.assertFalse(result['posting_allowed'])
        self.assertEqual(result['candidates'][0]['status'],'corporate_action_review_required')

    def test_wrong_cash_fee_currency_time_state_rejected(self):
        row, op = fixture()
        for key, value in [('payment',cash('-100.01')),('commission',cash('-1.26')),
                           ('price',cash('100','usd')),('date','2024-02-06T11:05:10Z'),
                           ('state','OPERATION_STATE_CANCELED'),('quantityDone','2.5')]:
            bad = dict(op); bad[key] = value
            self.assertIsNone(candidate(row,bad), key)

    def test_source_and_broker_ambiguity_and_consumed_operations(self):
        row, op = fixture()
        other = copy.deepcopy(row); other.update(id='source:2',record=2)
        source = {'audit':{'sha256':'source'},'rows':[row,other]}
        matches = {'rows':[{'source_record':i,'status':'unmatched'} for i in (1,2)],
                   'broker_operations':{'a:op':{'raw':op}}}
        self.assertEqual([r['status'] for r in audit(source,matches)['candidates']],['ambiguous','ambiguous'])
        matches['rows'].append({'source_record':3,'status':'matched','broker_operation_id':'a:op'})
        self.assertEqual(audit(source,matches)['candidates'],[])
        matches['rows'].pop();source['rows']=[row]
        other_op=dict(op,id='op2'); matches['broker_operations']['a:op2']={'raw':other_op}
        self.assertEqual([r['status'] for r in audit(source,matches)['candidates']],['ambiguous','ambiguous'])
