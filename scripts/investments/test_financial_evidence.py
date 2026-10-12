import copy
import unittest
from decimal import Decimal
from financial_evidence import audit_cash, audit_income, audit_trade, amount, financial_evidence, evidence_mapping, income_evidence_mapping, cash_evidence_mapping


def cash(value, currency='rub'):
    value = Decimal(value)
    units = int(value)
    return {'units': str(units), 'nano': int((value - units) * 10**9), 'currency': currency}


def fixture(bond=False):
    row = {'id': 'source:1', 'record': 1,
           'raw': {'Event': 'BUY', 'Currency': 'RUB', 'DoNotAdjustCash': 'False'},
           'decimals': {'Quantity': '3', 'Price': '10.123456789', 'FeeTax': '.01', 'NKD': '.125' if bond else '0'}}
    broker = {'instrumentType': 'bond' if bond else 'share', 'instrumentUid': 'uid',
              'quantityDone': '3', 'price': cash('10.123456789'),
              'payment': cash('-30.75' if bond else '-30.37'), 'commission': cash('-.01')}
    if bond:
        broker['accruedInt'] = cash('.38')
    return row, broker


class FinancialEvidenceTests(unittest.TestCase):
    def test_actual_payment_and_separate_fee(self):
        row, broker = fixture()
        result = audit_trade(row, broker)
        self.assertEqual(result['status'], 'amounts_reconciled')
        self.assertEqual(result['facts']['actual_clean_principal'], '30.37')
        self.assertEqual(result['facts']['broker_fee'], '0.01')
        self.assertEqual(result['facts']['cash_equality'], 'minor_unit_half_up')
        self.assertEqual(result['facts']['gross_rounding_adjustment'], '-0.000370367')

    def test_total_bond_accrued_is_not_multiplied_twice(self):
        row, broker = fixture(True)
        result = audit_trade(row, broker)
        self.assertEqual(result['status'], 'amounts_reconciled')
        self.assertEqual(result['facts']['broker_total_accrued'], '0.38')
        self.assertEqual(result['facts']['actual_clean_principal'], '30.37')
        self.assertEqual(result['facts']['source_gross_product'], '30.745370367')
        self.assertEqual(row['decimals']['NKD'], '.125')

    def test_rounded_average_needs_exact_cash(self):
        row,broker=fixture()
        row['decimals'].update(Quantity='1600',Price='3.46090625',FeeTax='16.61')
        broker.update(quantityDone='1600',price=cash('3.460906'),payment=cash('-5537.45'),commission=cash('-16.61'))
        result=audit_trade(row,broker)
        self.assertEqual(result['status'],'amounts_reconciled')
        self.assertEqual(result['facts']['price_equality'],'rounded_average_from_actual_cash')
        broker['payment']=cash('-5537.46')
        self.assertEqual(audit_trade(row,broker)['status'],'unresolved')

    def test_source_and_broker_averages_round_same_actual_cash(self):
        row,broker=fixture()
        row['decimals'].update(Quantity='12',Price='531.40833333',FeeTax='2.55')
        broker.update(quantityDone='12',price=cash('531.408333'),payment=cash('-6376.90'),commission=cash('-2.55'))
        result=audit_trade(row,broker)
        self.assertEqual(result['status'],'amounts_reconciled')
        self.assertEqual(result['facts']['actual_clean_principal'],'6376.9')
        row['decimals']['Price']='531.40833332'
        self.assertEqual(audit_trade(row,broker)['status'],'unresolved')

    def test_missing_accrued_is_not_zero(self):
        row, broker = fixture(True)
        del broker['accruedInt']
        result = audit_trade(row, broker)
        self.assertEqual(result['status'], 'unresolved')
        self.assertIn('missing_or_wrong_currency_broker_money', result['issues'])

    def test_gross_mismatch_sign_and_suppression_block(self):
        for field, value, code in [('payment', cash('-30.36'), 'gross_cash_not_reconciled'),
                                   ('payment', cash('30.37'), 'unexpected_payment_sign'),
                                   ('commission', cash('.01'), 'commission_mismatch')]:
            row, broker = fixture()
            broker[field] = value
            result = audit_trade(row, broker)
            self.assertEqual(result['status'], 'unresolved')
            self.assertIn(code, result['issues'])
        row, broker = fixture()
        row['raw']['DoNotAdjustCash'] = 'True'
        self.assertEqual(audit_trade(row, broker)['status'], 'unresolved')

    def test_wrong_currency_and_fractional_nano_rejected(self):
        for money in [cash('1', 'usd'), {'units': '1', 'nano': 0.5, 'currency': 'rub'},
                      {'units': '1', 'nano': -1, 'currency': 'rub'},
                      {'units': '9223372036854775808', 'currency': 'rub'}]:
            with self.assertRaises(ValueError):
                amount(money, 'RUB')

    def test_large_exact_principal_is_not_float(self):
        row, broker = fixture()
        row['decimals'].update(Quantity='1', Price='9007199254740993.123456789')
        broker.update(quantityDone='1', price=cash(row['decimals']['Price']), payment=cash('-9007199254740993.12'))
        result = audit_trade(row, broker)
        self.assertEqual(result['status'], 'amounts_reconciled')
        self.assertEqual(result['facts']['actual_clean_principal'], '9007199254740993.12')
        self.assertEqual(result['facts']['gross_rounding_adjustment'], '-0.003456789')

    def test_every_source_row_remains_in_report(self):
        row, broker = fixture()
        other = copy.deepcopy(row)
        other.update(record=2, id='source:2')
        source = {'audit': {'sha256': 'source'}, 'rows': [row, other]}
        matched = {'rows': [{'source_record': 1, 'account_id': 'account', 'broker_operation_id': 'op', 'status': 'matched'},
                            {'source_record': 2, 'account_id': 'other', 'status': 'unmatched'}],
                   'broker_operations': {'op': {'raw': broker}}, 'linked_fee_tax_operations': {}}
        result = financial_evidence(source, matched)
        self.assertEqual(len(result['rows']), 2)
        self.assertEqual(result['summary']['by_status'], {'amounts_reconciled': 1, 'unresolved': 1})
        self.assertEqual(result['rows'][1]['issues'], ['no_unique_broker_match'])

    def test_evidence_export_keeps_strings_and_snapshot_identity(self):
        row, broker = fixture(True)
        broker.update(brokerAccountId='account', id='trade', ticker='TEST', type='OPERATION_TYPE_BUY', state='OPERATION_STATE_EXECUTED', date='2024-01-01T12:00:00Z')
        report = {'hash': 'report-hash', 'rows': [{'status': 'amounts_reconciled', 'record': 1, 'source_row_id': 'source:1', 'broker_operation_id': 'op', 'time_quality': 'matched_date_only', 'facts': {'currency': 'RUB'}}]}
        exported = evidence_mapping({'audit': {'sha256': 'source'}}, {'broker_operations': {'op': {'raw': broker}}}, report, {'account': 'snapshot-hash'})
        report['rows'].append({'status': 'income_amounts_reconciled', 'record': 2})
        self.assertEqual(len(evidence_mapping({'audit': {'sha256': 'source'}}, {'broker_operations': {'op': {'raw': broker}}}, report, {'account': 'snapshot-hash'})['trade_evidence']), 1)
        broker['tradesInfo']={'trades':[{'num':'execution','date':broker['date'],'quantity':'3','price':broker['price']}]}
        execution=evidence_mapping({'audit':{'sha256':'source'}},{'broker_operations':{'op':{'raw':broker}}},report,{'account':'snapshot-hash'},True)['trade_evidence'][1]
        self.assertTrue(execution['use_execution_time'])
        self.assertEqual(execution['executions'][0]['price'],'10.123456789')
        broker['tradesInfo']['trades'] = [
            {'num':'a','date':broker['date'],'quantity':'1','price':cash('10.123456787')},
            {'num':'b','date':broker['date'],'quantity':'2','price':cash('10.123456790')}]
        weighted=evidence_mapping({'audit':{'sha256':'source'}},{'broker_operations':{'op':{'raw':broker}}},report,{'account':'snapshot-hash'},True)['trade_evidence'][1]
        self.assertTrue(weighted['use_execution_time'])
        self.assertEqual(len(weighted['executions']),2)
        broker['tradesInfo']['trades'][1]['price']=cash('10.123456791')
        wrong=evidence_mapping({'audit':{'sha256':'source'}},{'broker_operations':{'op':{'raw':broker}}},report,{'account':'snapshot-hash'},True)['trade_evidence'][1]
        self.assertNotIn('use_execution_time',wrong)
        broker['tradesInfo']['trades'][1]['price']=cash('10.123456790')
        broker['tradesInfo']['trades'].append(dict(broker['tradesInfo']['trades'][0]))
        ambiguous=evidence_mapping({'audit':{'sha256':'source'}},{'broker_operations':{'op':{'raw':broker}}},report,{'account':'snapshot-hash'},True)['trade_evidence'][1]
        self.assertNotIn('use_execution_time',ambiguous)
        proof = exported['trade_evidence'][1]
        self.assertEqual(proof['payment'], '-30.75')
        self.assertEqual(proof['accrued'], '0.38')
        self.assertEqual(proof['snapshot_sha256'], 'snapshot-hash')
        self.assertTrue(proof['date_only'])


class IncomeEvidenceTests(unittest.TestCase):
    def fixture(self, coupon=False):
        row = {'raw': {'Event': 'DIVIDEND', 'Date': '2024-01-01 15:00:00', 'Symbol': 'TEST',
                       'Currency': 'RUB', 'FeeCurrency': '', 'DoNotAdjustCash': 'False'},
               'decimals': {'Quantity': '100.123456789', 'Price': '0', 'FeeTax': '13', 'NKD': '0'}}
        broker = {'id': 'credit', 'brokerAccountId': 'account', 'instrumentUid': 'uid', 'ticker': 'TEST',
                  'type': 'OPERATION_TYPE_COUPON' if coupon else 'OPERATION_TYPE_DIVIDEND',
                  'state': 'OPERATION_STATE_EXECUTED', 'date': '2024-01-01T12:00:00Z',
                  'payment': cash('100.123456789')}
        tax = dict(broker, id='tax', type='OPERATION_TYPE_BOND_TAX' if coupon else 'OPERATION_TYPE_DIVIDEND_TAX', payment=cash('-13'))
        return row, broker, [('account:tax', tax)]

    def test_income_cash_and_withholding_are_separate(self):
        for coupon in [False, True]:
            row, broker, taxes = self.fixture(coupon)
            result = audit_income(row, broker, taxes)
            self.assertEqual(result['status'], 'income_amounts_reconciled')
            self.assertEqual(result['facts']['net_cash'], '87.123456789')
            self.assertEqual(result['facts']['income_basis'], 'gross')
            self.assertEqual(result['facts']['kind'], 'coupon' if coupon else 'dividend')
            if coupon:
                self.assertFalse(result['facts']['coupon_allocation_confirmed'])

    def test_income_export_preserves_both_broker_operations(self):
        row, broker, taxes = self.fixture()
        report = {'hash': 'report', 'rows': [{'status': 'income_amounts_reconciled', 'record': 1,
                  'source_row_id': 'source:1', 'broker_operation_id': 'credit',
                  'linked_fee_tax_operations': ['tax']}]}
        matches = {'broker_operations': {'credit': {'raw': broker}, 'tax': {'raw': taxes[0][1]}}}
        result = income_evidence_mapping({'audit': {'sha256': 'source'}}, matches, report, {'account': 'snapshot'})
        proof = result['income_evidence'][1]
        self.assertEqual(proof['credit']['payment'], '100.123456789')
        self.assertEqual(proof['tax']['payment'], '-13')
        self.assertEqual(proof['credit']['id'], 'credit')
        self.assertEqual(proof['tax']['id'], 'tax')
        self.assertEqual(proof['snapshot_sha256'], 'snapshot')

    def test_withholding_cannot_also_be_a_source_operation(self):
        row, broker, taxes = self.fixture()
        result = audit_income(row, broker, taxes, ['account:tax'])
        self.assertIn('tax_already_matched_source_row', result['issues'])
        self.assertEqual(result['status'], 'unresolved')

    def test_zero_withholding_is_not_tax_exemption(self):
        row, broker, _ = self.fixture()
        row['decimals']['FeeTax'] = '0'
        result = audit_income(row, broker, [])
        self.assertEqual(result['status'], 'unresolved')
        self.assertIn('zero_withholding_requires_review', result['issues'])

    def test_observed_income_is_explicitly_tax_pending(self):
        row,broker,_=self.fixture(True);row['decimals']['FeeTax']='0'
        result=audit_income(row,broker,[],include_observed_income=True)
        self.assertEqual(result['status'],'income_payment_reconciled_tax_pending')
        self.assertEqual(result['facts']['net_cash'],'100.123456789')
        result.update(record=1,source_row_id='source:1',broker_operation_id='credit',linked_fee_tax_operations=[])
        exported=income_evidence_mapping({'audit':{'sha256':'source'}},{'broker_operations':{'credit':{'raw':broker}}},{'hash':'report','rows':[result]},{'account':'snapshot'})
        self.assertTrue(exported['income_evidence'][1]['tax_review_required'])
        self.assertEqual(exported['income_evidence'][1]['tax'],{})

    def test_nonzero_price_requires_explicit_metadata_policy(self):
        row,broker,taxes=self.fixture();row['decimals']['Price']='94.52'
        self.assertEqual(audit_income(row,broker,taxes)['status'],'unresolved')
        result=audit_income(row,broker,taxes,retain_income_price_metadata=True)
        self.assertEqual(result['status'],'income_amounts_reconciled')
        self.assertEqual(result['facts']['source_price_metadata'],'94.52')
        self.assertEqual(result['facts']['net_cash'],'87.123456789')

    def test_no_ambiguous_or_missing_tax_link(self):
        row, broker, taxes = self.fixture()
        for values in [[], taxes+taxes]:
            result = audit_income(row, broker, values)
            self.assertEqual(result['status'], 'unresolved')
            self.assertIn('income_tax_link_required', result['issues'])

    def test_wrong_tax_account_currency_sign_type_and_time(self):
        for field, value in [('brokerAccountId', 'other'), ('payment', cash('-13', 'usd')),
                             ('payment', cash('13')), ('type', 'OPERATION_TYPE_BOND_TAX'),
                             ('date', '2024-01-01T12:00:01Z'), ('ticker', 'OTHER')]:
            with self.subTest(field=field, value=value):
                row, broker, taxes = self.fixture()
                taxes[0][1][field] = value
                self.assertEqual(audit_income(row, broker, taxes)['status'], 'unresolved')

    def test_price_cash_suppression_and_accrued_not_discarded(self):
        for section, field, value in [('decimals', 'Price', '1'), ('decimals', 'NKD', '1'),
                                      ('raw', 'DoNotAdjustCash', 'True'), ('decimals', 'Quantity', '101')]:
            row, broker, taxes = self.fixture()
            row[section][field] = value
            self.assertEqual(audit_income(row, broker, taxes)['status'], 'unresolved')


class CashEvidenceTests(unittest.TestCase):
    def fixture(self, event='CASH_IN'):
        tax = event == 'TAX'
        row = {'raw': {'Event': event, 'Date': '2024-01-01 15:00:00', 'Symbol': '' if tax else 'RUB',
                       'Currency': 'RUB', 'FeeCurrency': '', 'DoNotAdjustCash': 'False'},
               'decimals': {'Quantity': '0' if tax else '100.123456789', 'Price': '0' if tax else '1',
                            'FeeTax': '13' if tax else '0', 'NKD': '0'}}
        broker = {'type': {'CASH_IN':'OPERATION_TYPE_INPUT', 'CASH_GAIN':'OPERATION_TYPE_OVERNIGHT', 'TAX':'OPERATION_TYPE_TAX'}[event],
                  'state': 'OPERATION_STATE_EXECUTED', 'date': '2024-01-01T12:00:00Z',
                  'payment': cash('-13' if tax else '100.123456789')}
        return row, broker

    def test_cash_and_tax_have_correct_sign_and_source_basis(self):
        for event, kind, basis in [('CASH_IN','deposit','quantity'),('TAX','tax','fee_tax')]:
            row,broker = self.fixture(event)
            result = audit_cash(row,broker)
            self.assertEqual(result['status'],'cash_amounts_reconciled')
            self.assertEqual(result['facts']['kind'],kind)
            self.assertEqual(result['facts']['amount_basis'],basis)
            self.assertEqual(result['facts']['external_flow'], event == 'CASH_IN')

    def test_repo_instrument_is_preserved_and_requires_mapping(self):
        row,broker = self.fixture('CASH_GAIN')
        broker.update(ticker='CBOM',instrumentUid='uid',instrumentType='share')
        result = audit_cash(row,broker)
        self.assertEqual(result['status'],'unresolved')
        self.assertIn('repo_income_instrument_mapping_required',result['issues'])
        self.assertEqual(result['facts']['broker_instrument_uid'],'uid')
        self.assertFalse(result['facts']['external_flow'])

    def test_repo_export_keeps_security_and_exact_amount(self):
        row,broker=self.fixture('CASH_GAIN')
        broker.update(id='repo',brokerAccountId='account',ticker='CBOM',instrumentUid='11111111-1111-4111-8111-111111111111',instrumentType='share')
        result=audit_cash(row,broker)
        self.assertEqual(result['status'],'cash_amounts_reconciled')
        self.assertEqual(result['facts']['kind'],'repo_income')
        result.update(record=1,source_row_id='source:1',broker_operation_id='repo')
        exported=cash_evidence_mapping({'audit':{'sha256':'source'}},{'broker_operations':{'repo':{'raw':broker}}},{'hash':'report','rows':[result]},{'account':'snapshot'})
        proof=exported['cash_evidence'][1]
        self.assertEqual(proof['instrument_uid'],broker['instrumentUid'])
        self.assertEqual(proof['operation']['payment'],'100.123456789')

    def test_wrong_sign_currency_time_and_extra_components_block(self):
        for field,value in [('payment',cash('-100.123456789')),('payment',cash('100.123456789','usd')),
                            ('date','2024-01-01T12:00:01Z'),('commission',cash('-1')),
                            ('type','OPERATION_TYPE_OVERNIGHT'),('state','OPERATION_STATE_CANCELED')]:
            row,broker = self.fixture();broker[field]=value
            self.assertEqual(audit_cash(row,broker)['status'],'unresolved')
        for field,value in [('FeeTax','1'),('NKD','1'),('Price','2')]:
            row,broker=self.fixture();row['decimals'][field]=value
            self.assertEqual(audit_cash(row,broker)['status'],'unresolved')


if __name__ == '__main__':
    unittest.main()
