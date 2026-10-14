import copy
import unittest
from reconcile import reconcile, money
from decimal import Decimal


def source(rows):
    return {'version': 1, 'scope': 'combined_unassigned', 'audit': {'sha256': 'source'}, 'rows': rows}

def row(identity='r1', time='2026-01-01 13:00:00'):
    return {'id': identity, 'record': 1, 'raw': {'Event': 'BUY', 'Symbol': 'TEST', 'Date': time, 'Currency': 'USD', 'FeeCurrency': ''},
            'decimals': {'Price': '0.123456789', 'Quantity': '3', 'FeeTax': '0.01'}}

def op(identity='o1', account='a'):
    return {'id': identity, 'brokerAccountId': account, 'state': 'OPERATION_STATE_EXECUTED', 'type': 'OPERATION_TYPE_BUY',
            'date': '2026-01-01T10:00:00.123Z', 'ticker': 'TEST', 'quantityDone': '3',
            'payment': {'currency': 'usd', 'units': '0', 'nano': -370370367},
            'price': {'currency': 'usd', 'units': '0', 'nano': 123456789},
            'commission': {'currency': 'usd', 'units': '0', 'nano': -10000000}}

def snapshot(operations, account='a'):
    return {'account': {'id': account, 'type': 'ACCOUNT_TYPE_TINKOFF'}, 'operations': operations}

class ReconciliationTest(unittest.TestCase):
    def test_exact(self):
        result = reconcile(source([row()]), [snapshot([op()])])
        self.assertEqual(result['rows'][0]['account_id'], 'tinvest:a')
        self.assertEqual(result['rows'][0]['status'], 'matched')
        self.assertEqual(len(result['accounts']), 2)

    def test_duplicate_source_not_silently_assigned(self):
        result = reconcile(source([row(), row('r2')]), [snapshot([op()])])
        self.assertTrue(all(r['status'] == 'ambiguous' for r in result['rows']))
        self.assertEqual(len(result['rows']), 2)

    def test_identical_operations_across_accounts(self):
        result = reconcile(source([row()]), [snapshot([op()]), snapshot([op(account='b')], 'b')])
        self.assertEqual(result['rows'][0]['status'], 'ambiguous')

    def test_date_only_unique(self):
        result = reconcile(source([row(time='2026-01-01 00:00:00')]), [snapshot([op()])])
        self.assertEqual(result['rows'][0]['status'], 'matched_date_only')

    def test_date_only_cannot_reuse_precise_match(self):
        result = reconcile(source([row(), row('r2', '2026-01-01 00:00:00')]), [snapshot([op()])])
        self.assertFalse(result['matched_broker_operations'])

    def test_non_midnight_time_mismatch_requires_review(self):
        result = reconcile(source([row(time='2026-01-01 13:00:01')]), [snapshot([op()])])
        self.assertEqual(result['rows'][0]['status'], 'same_day_review')

    def test_cancelled_trade_not_matched(self):
        operation = op()
        operation['state'] = 'OPERATION_STATE_CANCELED'
        result = reconcile(source([row()]), [snapshot([operation])])
        self.assertEqual(result['rows'][0]['status'], 'unmatched')

    def test_missing_commission_does_not_hide_proven_account_identity(self):
        operation = op()
        del operation['commission']
        result = reconcile(source([row()]), [snapshot([operation])])
        self.assertEqual(result['rows'][0]['account_id'], 'tinvest:a')
        self.assertEqual(result['rows'][0]['financial_validation'], 'pending')
        self.assertEqual(result['broker_operations']['tinvest:a:o1']['raw'], operation)
        self.assertNotIn('commission', operation)

    def test_missing_commission_requires_independent_gross_payment_match(self):
        operation = op()
        del operation['commission']
        operation['payment']['nano'] = -400000000
        result = reconcile(source([row()]), [snapshot([operation])])
        self.assertEqual(result['rows'][0]['status'], 'unmatched')

    def test_missing_commission_still_cannot_choose_between_accounts(self):
        first, second = op(), op(account='b')
        del first['commission'], second['commission']
        result = reconcile(source([row()]), [snapshot([first]), snapshot([second], 'b')])
        self.assertEqual(result['rows'][0]['status'], 'ambiguous')

    def test_child_fee_is_linked_not_new_expense(self):
        fee = copy.deepcopy(op('fee'))
        fee.update(type='OPERATION_TYPE_BROKER_FEE', parentOperationId='o1', payment={'currency': 'usd', 'units': '0', 'nano': -10000000})
        result = reconcile(source([row()]), [snapshot([op(), fee])])
        self.assertEqual(result['linked_fee_tax_operations'], {'r1': ['tinvest:a:fee']})
        self.assertEqual(result['unmatched_broker_operations'], [])

    def test_tax_shared_by_two_payouts_is_not_first_row_wins(self):
        first, second = row(), row('r2')
        first['raw']['Event'] = second['raw']['Event'] = 'DIVIDEND'
        first['decimals']['Quantity'], second['decimals']['Quantity'] = '10', '11'
        second['record'] = 2
        a, b, tax = op(), op('o2'), op('tax')
        a.update(type='OPERATION_TYPE_DIVIDEND', payment={'currency':'usd', 'units':'10'})
        b.update(type='OPERATION_TYPE_DIVIDEND', payment={'currency':'usd', 'units':'11'})
        tax.update(type='OPERATION_TYPE_DIVIDEND_TAX', payment={'currency':'usd', 'units':'0', 'nano':-10000000})
        for values in [[first,second],[second,first]]:
            result = reconcile(source(values), [snapshot([a,b,tax])])
            self.assertEqual(len(result['matched_broker_operations']), 2)
            self.assertEqual(result['linked_fee_tax_operations'], {})
            self.assertIn('tinvest:a:tax', result['unmatched_broker_operations'])

    def test_account_contamination_and_conflict_rejected(self):
        with self.assertRaises(ValueError):
            reconcile(source([row()]), [snapshot([op(account='b')])])
        other = op(); other['quantityDone'] = '2'
        with self.assertRaises(ValueError):
            reconcile(source([row()]), [snapshot([op(), other])])

    def test_decimal_not_float(self):
        self.assertEqual(money({'units': '-2', 'nano': -123456789}), Decimal('-2.123456789'))

if __name__ == '__main__':
    unittest.main()
