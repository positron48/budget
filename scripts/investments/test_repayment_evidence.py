import unittest
from financial_evidence import audit_repayment
from test_financial_evidence import cash

class RepaymentEvidenceTests(unittest.TestCase):
    def fixture(self):
        return ({'raw':{'Event':'REPAYMENT','Currency':'RUB','Symbol':'BOND','DoNotAdjustCash':'False'},
                 'decimals':{'Quantity':'2','Price':'106076.2','FeeTax':'0','NKD':''}},
                {'type':'OPERATION_TYPE_BOND_REPAYMENT_FULL','state':'OPERATION_STATE_EXECUTED',
                 'instrumentType':'bond','instrumentUid':'uid','ticker':'BOND',
                 'quantityDone':'0','payment':cash('212152.4')})

    def test_zero_broker_quantity_does_not_mean_no_securities(self):
        row,op=self.fixture();result=audit_repayment(row,op)
        self.assertEqual(result['status'],'unresolved')
        self.assertEqual(result['facts']['principal_cash'],'212152.4')
        self.assertFalse(result['facts']['quantity_confirmed_by_operation'])
        self.assertEqual(result['issues'],['repayment_position_quantity_required','historical_nominal_terms_required'])
        op['quantityDone']='2';result=audit_repayment(row,op)
        self.assertEqual(result['issues'],['historical_nominal_terms_required'])

    def test_mismatch_cannot_be_exported_as_cash(self):
        row,op=self.fixture()
        for key,value in [('payment',cash('212152.41')),('payment',cash('212152.4','usd')),
                          ('quantityDone','3'),('commission',cash('-1')),('state','OPERATION_STATE_CANCELED')]:
            bad=dict(op);bad[key]=value;result=audit_repayment(row,bad)
            self.assertEqual(result['status'],'unresolved')
            self.assertEqual(result['facts'],{})
            self.assertTrue(result['issues'])
