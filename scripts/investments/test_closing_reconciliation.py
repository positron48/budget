import copy
import unittest
from closing_reconciliation import compare, money, timestamp

class ClosingComparisonTest(unittest.TestCase):
    def fixture(self):
        stamp='2024-01-01T00:00:00Z'
        ledger={'AsOf':stamp,'Positions':[{'InstrumentKey':'tinvest:s','Quantity':'9007199254740994'}],
                'Cash':[{'Currency':'USD','Settled':'100.000000001','Economic':'100.000000001','Receivable':'0','Payable':'0'}]}
        rehearsal={'SourceSHA256':'checksum','Accounts':[{'SourceAccount':'tinvest:a','Snapshot':ledger}]}
        broker={'account':{'id':'a'},'fetched_at':stamp,'positions':{'accountId':'a','securities':[{'instrumentUid':'s','positionUid':'p','figi':'f','balance':'9007199254740993','blocked':'1'}], 'money':[{'currency':'usd','units':'100','nano':1}]},'portfolio':{'accountId':'a','positions':[]}}
        return rehearsal,broker
    def test_exact_total_includes_blocked_security(self):
        r,b=self.fixture();out=compare(r,[b],{});self.assertEqual(out['summary']['position_mismatches'],0);self.assertEqual(out['summary']['cash_mismatches'],0);self.assertFalse(out['cutover_allowed'])
    def test_cash_blocking_does_not_invent_total(self):
        r,b=self.fixture();b['positions']['blocked']=[{'currency':'usd','units':'1','nano':0}];out=compare(r,[b],{});self.assertIsNone(out['accounts'][0]['cash'][0]['matches'])
    def test_time_mismatch_and_missing_ledger_are_explicit(self):
        r,b=self.fixture();b['fetched_at']='2024-01-02T00:00:00Z';r['Accounts'].append({'SourceAccount':'other-brokers'});out=compare(r,[b],{});self.assertIn('as_of_mismatch',out['accounts'][0]['issues']);self.assertEqual(out['accounts'][1]['state'],'not_compared')
    def test_position_identity_requires_matching_evidence(self):
        r,b=self.fixture();r['Accounts'][0]['Snapshot']['Positions'][0]['InstrumentKey']='tinvest-position:p';out=compare(r,[b],{'s':{'position_uid':'p','figi':'f'}});self.assertEqual(out['summary']['position_mismatches'],0)
        b['positions']['securities'][0]['figi']='different';out=compare(r,[b],{'s':{'position_uid':'p','figi':'f'}});self.assertIn('instrument_identity_evidence_mismatch',out['accounts'][0]['issues'])
    def test_duplicate_and_foreign_account_rejected(self):
        r,b=self.fixture()
        with self.assertRaises(ValueError):compare(r,[b,copy.deepcopy(b)],{})
        b['positions']['accountId']='other'
        with self.assertRaises(ValueError):compare(r,[b],{})
    def test_time_keeps_nanoseconds_and_normalizes_zone(self):
        self.assertEqual(timestamp('2024-01-01T00:00:00Z'),timestamp('2024-01-01T03:00:00+03:00'))
        self.assertNotEqual(timestamp('2024-01-01T00:00:00.000000001Z'),timestamp('2024-01-01T00:00:00.000000002Z'))
    def test_invalid_money(self):
        for v in [{'units':'1','nano':-1},{'units':'0','nano':1000000000},{'units':'NaN','nano':0}]:
            with self.assertRaises(ValueError):money(v)

    def test_duplicate_portfolio_identity_rejected(self):
        r,b=self.fixture()
        b['portfolio']['positions']=[{'instrumentUid':'s'}, {'instrumentUid':'s'}]
        with self.assertRaises(ValueError):compare(r,[b],{})

if __name__=='__main__':unittest.main()
