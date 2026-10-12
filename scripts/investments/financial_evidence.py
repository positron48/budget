#!/usr/bin/env python3
"""Reconcile Snowball monetary facts against immutable broker snapshots; no posting."""
import argparse
from collections import Counter
from decimal import Decimal, localcontext, ROUND_HALF_UP
import hashlib
import json
import os
import re
import uuid
from pathlib import Path
from position_evidence import position_before
from reconcile import read_verified_source, reconcile, walltime, trade_price_matches


def amount(value, currency):
    if not isinstance(value, dict) or value.get('currency', '').upper() != currency:
        raise ValueError('missing_or_wrong_currency_broker_money')
    raw_units, nano = value.get('units', '0'), value.get('nano', 0)
    if not isinstance(raw_units, (str, int)) or isinstance(raw_units, bool) or not re.fullmatch(r'-?[0-9]{1,20}', str(raw_units)) or not isinstance(nano, int) or isinstance(nano, bool):
        raise ValueError('invalid_broker_money')
    units = int(raw_units)
    if not -(2**63) <= units < 2**63:
        raise ValueError('invalid_broker_money')
    if abs(nano) >= 10**9 or units * nano < 0:
        raise ValueError('invalid_broker_money')
    return Decimal(units) + Decimal(nano) / Decimal(10**9)


def exact(value):
    return format(value, 'f')


def cent(value):
    return value.quantize(Decimal('.01'), rounding=ROUND_HALF_UP)


def audit_trade(row, broker):
    with localcontext() as ctx:
        ctx.prec = 1024
        return _audit_trade(row, broker)


def _audit_trade(row, broker):
    raw, dec = row['raw'], row['decimals']
    event, currency = raw['Event'], raw['Currency'].upper()
    out = {'status': 'unresolved', 'issues': [], 'facts': {}}
    issue = out['issues'].append
    if broker.get('instrumentType') not in {'share', 'etf', 'bond'}:
        issue('unsupported_instrument_type')
    if not broker.get('instrumentUid'):
        issue('missing_instrument_identity')
    if currency not in {'RUB', 'USD', 'EUR'}:
        issue('unsupported_currency_minor_units')
    q, price = Decimal(dec['Quantity']), Decimal(dec['Price'])
    nkd, fee = Decimal(dec.get('NKD') or '0'), Decimal(dec['FeeTax'])
    if q <= 0 or price <= 0 or nkd < 0 or fee < 0:
        issue('invalid_source_money_or_quantity')
    if raw.get('DoNotAdjustCash') != 'False':
        issue('cash_suppression_requires_review')
    try:
        amount(broker.get('price'), currency)
        payment = amount(broker.get('payment'), currency)
        commission = amount(broker.get('commission'), currency)
        if (event == 'BUY' and payment >= 0) or (event == 'SELL' and payment <= 0):
            issue('unexpected_payment_sign')
        if commission > 0 or -commission != fee:
            issue('commission_mismatch')
        if Decimal(broker.get('quantityDone', '0')) != q or not trade_price_matches(row, broker):
            issue('broker_quantity_or_price_changed')
        accrued = Decimal(0)
        if broker.get('instrumentType') == 'bond':
            accrued = amount(broker.get('accruedInt'), currency)
            if accrued < 0 or accrued != cent(nkd * q):
                issue('accrued_total_not_reconciled')
        elif nkd != 0:
            issue('unexpected_source_accrued')
        elif 'accruedInt' in broker and amount(broker['accruedInt'], currency) != 0:
            issue('unexpected_broker_accrued')
        gross = abs(payment)
        calculated = q * price + nkd * q
        if gross != calculated and gross != cent(calculated):
            issue('gross_cash_not_reconciled')
        principal = gross - accrued
        if principal <= 0:
            issue('nonpositive_clean_principal')
        out['facts'] = {
            'currency': currency, 'quantity_units': exact(q),
            'source_price': exact(price), 'broker_price': exact(amount(broker['price'], currency)),
            'price_equality': 'exact' if price == amount(broker['price'], currency) else 'rounded_average_from_actual_cash', 'source_accrued_per_unit': exact(nkd),
            'source_clean_product': exact(q * price),
            'source_gross_product': exact(calculated),
            'broker_signed_payment': exact(payment), 'broker_fee': exact(-commission),
            'broker_total_accrued': exact(accrued), 'actual_clean_principal': exact(principal),
            'gross_rounding_adjustment': exact(gross - calculated),
            'clean_product_adjustment': exact(principal - q * price),
            'cash_equality': 'exact' if gross == calculated else 'minor_unit_half_up' if gross == cent(calculated) else 'mismatch',
        }
        if not out['issues']:
            out['status'] = 'amounts_reconciled'
    except (ValueError, TypeError, ArithmeticError) as error:
        issue(str(error) if isinstance(error, ValueError) else 'invalid_broker_amount')
    return out


def audit_income(row, broker, tax_operations, primary_operations=(), include_observed_income=False, retain_income_price_metadata=False):
    """Verify actual cash and a distinct withholding; missing tax is not exemption."""
    with localcontext() as ctx:
        ctx.prec = 1024
        out = {'status': 'unresolved', 'issues': [], 'facts': {}}
        issue = out['issues'].append
        raw, dec = row['raw'], row['decimals']
        currency = raw['Currency'].upper()
        kind = {'OPERATION_TYPE_DIVIDEND': 'dividend', 'OPERATION_TYPE_COUPON': 'coupon'}.get(broker.get('type'))
        if raw['Event'] != 'DIVIDEND' or not kind or broker.get('state') != 'OPERATION_STATE_EXECUTED':
            issue('income_event_or_state_mismatch')
        if not broker.get('instrumentUid') or broker.get('ticker') != raw.get('Symbol'):
            issue('income_instrument_mismatch')
        if currency not in {'RUB', 'USD', 'EUR'}:
            issue('unsupported_currency_minor_units')
        if raw.get('DoNotAdjustCash') != 'False':
            issue('cash_suppression_requires_review')
        if (raw.get('FeeCurrency') or currency).upper() != currency:
            issue('income_tax_currency_mismatch')
        try:
            if walltime(broker['date']) != raw['Date']:
                issue('income_time_requires_review')
            payment = amount(broker.get('payment'), currency)
            source_amount = Decimal(dec['Quantity'])
            tax = Decimal(dec['FeeTax'])
            if payment <= 0 or payment != source_amount:
                issue('income_cash_mismatch')
            if tax < 0 or tax > payment or Decimal(dec.get('NKD') or '0') != 0:
                issue('invalid_income_components')
            # A nonzero source Price has an unresolved meaning even when Quantity
            # matches the credit. Never silently discard that extra monetary field.
            if Decimal(dec['Price']) < 0 or (Decimal(dec['Price']) != 0 and not retain_income_price_metadata):
                issue('income_price_requires_review')
            if 'commission' in broker and amount(broker['commission'], currency) != 0:
                issue('income_commission_requires_review')
            if tax == 0 and not include_observed_income:
                issue('zero_withholding_requires_review')
            if tax > 0 and len(tax_operations) != 1 or tax == 0 and tax_operations:
                issue('income_tax_link_required')
            for identity, item in tax_operations:
                expected_type = 'OPERATION_TYPE_BOND_TAX' if kind == 'coupon' else 'OPERATION_TYPE_DIVIDEND_TAX'
                if identity in primary_operations:
                    issue('tax_already_matched_source_row')
                if (item.get('type') != expected_type or item.get('state') != 'OPERATION_STATE_EXECUTED'
                        or item.get('brokerAccountId') != broker.get('brokerAccountId')
                        or item.get('ticker') != broker.get('ticker')
                        or walltime(item['date']) != walltime(broker['date'])
                        or amount(item.get('payment'), currency) != -tax or tax <= 0):
                    issue('income_tax_link_mismatch')
            out['facts'] = {'kind': kind, 'currency': currency, 'source_amount_basis': 'quantity',
                            'income_basis': 'gross', 'source_price_metadata': exact(Decimal(dec['Price'])) if Decimal(dec['Price']) != 0 and retain_income_price_metadata else '', 'broker_signed_payment': exact(payment),
                            'source_withholding': exact(tax), 'net_cash': exact(payment-tax),
                            'coupon_allocation_confirmed': False if kind == 'coupon' else None}
            if not out['issues']:
                out['status'] = 'income_payment_reconciled_tax_pending' if tax == 0 else 'income_amounts_reconciled'
        except (ValueError, KeyError, TypeError, ArithmeticError):
            issue('invalid_income_evidence')
        return out


def audit_cash(row, broker):
    with localcontext() as ctx:
        ctx.prec = 1024
        out = {'status': 'unresolved', 'issues': [], 'facts': {}}
        issue = out['issues'].append
        raw, dec = row['raw'], row['decimals']
        currency = raw['Currency'].upper()
        contract = {'CASH_IN': ('OPERATION_TYPE_INPUT', 'deposit', 'quantity', 1),
                    'CASH_GAIN': ('OPERATION_TYPE_OVERNIGHT', 'cash_interest', 'quantity', 1),
                    'TAX': ('OPERATION_TYPE_TAX', 'tax', 'fee_tax', -1)}
        if raw['Event'] not in contract:
            issue('unsupported_cash_event')
            return out
        operation_type, kind, basis, sign = contract[raw['Event']]
        if broker.get('type') != operation_type or broker.get('state') != 'OPERATION_STATE_EXECUTED':
            issue('cash_event_or_state_mismatch')
        if raw.get('Symbol') not in {'', currency}:
            issue('cash_instrument_requires_review')
        if broker.get('ticker') or broker.get('instrumentUid'):
            if raw['Event'] == 'CASH_GAIN':
                kind = 'repo_income'
            if raw['Event'] == 'CASH_GAIN':
                try:
                    if str(uuid.UUID(broker.get('instrumentUid', ''))) != broker['instrumentUid'] or not broker.get('ticker') or broker.get('instrumentType') not in {'share','etf','bond'}:
                        issue('repo_income_instrument_mapping_required')
                except (ValueError, TypeError, KeyError):
                    issue('repo_income_instrument_mapping_required')
            else:
                issue('cash_instrument_requires_review')
        if currency not in {'RUB', 'USD', 'EUR'}:
            issue('unsupported_currency_minor_units')
        if raw.get('DoNotAdjustCash') != 'False':
            issue('cash_suppression_requires_review')
        try:
            if walltime(broker['date']) != raw['Date']:
                issue('cash_time_requires_review')
            payment = amount(broker.get('payment'), currency)
            price, quantity, fee = (Decimal(dec[key]) for key in ['Price', 'Quantity', 'FeeTax'])
            expected = quantity if basis == 'quantity' else fee
            if expected <= 0 or payment != sign * expected:
                issue('cash_payment_mismatch')
            if Decimal(dec.get('NKD') or '0') != 0:
                issue('unexpected_cash_accrued')
            if basis == 'quantity' and (price != 1 or fee != 0):
                issue('unallocated_cash_components')
            if basis == 'fee_tax' and (price != 0 or quantity != 0):
                issue('unallocated_cash_components')
            if (raw.get('FeeCurrency') or currency).upper() != currency:
                issue('cash_fee_currency_mismatch')
            if 'commission' in broker and amount(broker['commission'], currency) != 0:
                issue('cash_commission_requires_review')
            out['facts'] = {'kind': kind, 'currency': currency, 'amount_basis': basis,
                            'amount': exact(expected), 'broker_signed_payment': exact(payment),
                            'external_flow': kind == 'deposit',
                            'broker_instrument_uid': broker.get('instrumentUid', ''),
                            'broker_ticker': broker.get('ticker', '')}
            if not out['issues']:
                out['status'] = 'cash_amounts_reconciled'
        except (ValueError, KeyError, TypeError, ArithmeticError):
            issue('invalid_cash_evidence')
        return out


def audit_repayment(row, broker):
    """Separate confirmed principal cash from missing quantity/nominal evidence."""
    out = {'status': 'unresolved', 'issues': [], 'facts': {}}
    with localcontext() as ctx:
        ctx.prec = 1024
        try:
            raw, dec = row['raw'], row['decimals']
            currency = raw['Currency'].upper()
            kind = {'OPERATION_TYPE_BOND_REPAYMENT_FULL':'redemption',
                    'OPERATION_TYPE_BOND_REPAYMENT':'amortization'}.get(broker.get('type'))
            if (raw['Event'] != 'REPAYMENT' or not kind
                    or broker.get('state') != 'OPERATION_STATE_EXECUTED'
                    or broker.get('instrumentType') != 'bond'
                    or broker.get('ticker') != raw['Symbol']
                    or not broker.get('instrumentUid') or raw.get('DoNotAdjustCash') != 'False'):
                raise ValueError('invalid_repayment_identity')
            quantity, price = Decimal(dec['Quantity']), Decimal(dec['Price'])
            payment = amount(broker['payment'], currency)
            if not quantity.is_finite() or not price.is_finite() or min(quantity, price, payment) <= 0 or quantity * price != payment:
                raise ValueError('repayment_cash_mismatch')
            if Decimal(dec.get('NKD') or '0') != 0 or Decimal(dec['FeeTax']) != 0:
                raise ValueError('repayment_components_require_review')
            for field in ('commission','accruedInt'):
                if field in broker and amount(broker[field],currency) != 0:
                    raise ValueError('repayment_components_require_review')
            broker_quantity = Decimal(str(broker.get('quantityDone','0')))
            if not broker_quantity.is_finite() or broker_quantity < 0:
                raise ValueError('invalid_repayment_quantity')
            if broker_quantity > 0 and broker_quantity != quantity:
                raise ValueError('repayment_quantity_mismatch')
            out['facts'] = {'kind':kind,'currency':currency,'principal_cash':exact(payment),
                            'source_quantity':exact(quantity),'source_price':exact(price),
                            'broker_quantity':exact(broker_quantity),'cash_equality':'exact',
                            'quantity_confirmed_by_operation':broker_quantity > 0,
                            'nominal_confirmed':False}
            if broker_quantity == 0:
                out['issues'].append('repayment_position_quantity_required')
            out['issues'].append('historical_nominal_terms_required')
            # A matched cash payment alone must never produce a deposit or a
            # redemption with guessed nominal/currency or inferred closing lots.
        except (ValueError, KeyError, TypeError, ArithmeticError) as error:
            out['issues'].append(str(error) if isinstance(error,ValueError) else 'invalid_repayment_evidence')
    return out


def financial_evidence(source, reconciliation, include_observed_income=False, retain_income_price_metadata=False):
    links = {row['source_record']: row for row in reconciliation['rows']}
    result = {'version': 1, 'state': 'financial_evidence_only',
              'source_sha256': source['audit']['sha256'], 'rows': [],
              'limitations': ['No financial operations posted or existing source values rewritten.',
                              'Rounded equality is evidence for this matched row, not a general format rule.',
                              'Broker operation date is not proof of actual settlement time.',
                              'Unmatched source rows and broker-only operations remain unresolved.',
                              'Income, corporate actions, FIFO and full cash history require further reconciliation.']}
    for row in source['rows']:
        link = links[row['record']]
        item = {'record': row['record'], 'source_row_id': row['id'],
                'account_id': link['account_id'], 'status': 'unresolved', 'issues': []}
        if link.get('broker_operation_id'):
            identity = link['broker_operation_id']
            op = reconciliation['broker_operations'][identity]['raw']
            item['broker_operation_id'] = identity
            item['time_quality'] = link['status']
            item['broker_instrument_uid'] = op.get('instrumentUid', '')
            item['linked_fee_tax_operations'] = reconciliation['linked_fee_tax_operations'].get(row['id'], [])
            if row['raw']['Event'] in {'BUY', 'SELL'}:
                item.update(audit_trade(row, op))
            elif row['raw']['Event'] == 'DIVIDEND':
                taxes = [(identity, reconciliation['broker_operations'][identity]['raw']) for identity in item['linked_fee_tax_operations']]
                item.update(audit_income(row, op, taxes, reconciliation.get('matched_broker_operations', []), include_observed_income, retain_income_price_metadata))
            elif row['raw']['Event'] == 'REPAYMENT':
                item.update(audit_repayment(row, op))
                history = position_before([entry['raw'] for entry in reconciliation['broker_operations'].values()], op)
                item['position_evidence'] = history
                if history['quantity'] is not None:
                    history['matches_source_quantity'] = Decimal(history['quantity']) == Decimal(row['decimals']['Quantity'])
            elif row['raw']['Event'] in {'CASH_IN', 'CASH_GAIN', 'TAX'}:
                item.update(audit_cash(row, op))
            else:
                item['issues'].append('non_trade_financial_semantics_pending')
        else:
            item['issues'].append('no_unique_broker_match')
        result['rows'].append(item)
    result['summary'] = {'rows': len(result['rows']),
                         'by_status': dict(Counter(row['status'] for row in result['rows'])),
                         'issues': dict(Counter(issue for row in result['rows'] for issue in row['issues'])),
                         'cash_equality': dict(Counter(row.get('facts', {}).get('cash_equality', 'unavailable') for row in result['rows']))}
    return result


def evidence_mapping(source, matches, report, snapshot_hashes, use_execution_time=False):
    evidence = {}
    for row in report['rows']:
        if row['status'] != 'amounts_reconciled':
            continue
        op = matches['broker_operations'][row['broker_operation_id']]['raw']
        currency = row['facts']['currency']
        evidence[row['record']] = {
            'source_row_id': row['source_row_id'], 'broker_account_id': op['brokerAccountId'],
            'broker_operation_id': op['id'], 'snapshot_sha256': snapshot_hashes[op['brokerAccountId']],
            'report_hash': report['hash'], 'instrument_uid': op['instrumentUid'],
            'figi': op.get('figi',''), 'position_uid': op.get('positionUid',''),
            'instrument_type': op['instrumentType'], 'ticker': op['ticker'], 'currency': currency,
            'operation_type': op['type'], 'state': op['state'],
            'quantity': str(op['quantityDone']), 'price': exact(amount(op['price'], currency)),
            'payment': exact(amount(op['payment'], currency)), 'commission': exact(amount(op['commission'], currency)),
            'accrued': exact(amount(op['accruedInt'], currency)) if 'accruedInt' in op else None,
            'broker_date': op['date'], 'date_only': row['time_quality'] == 'matched_date_only',
            'reason': 'Broker payment, accrued interest and separate commission reconciled against the source; settlement time remains unconfirmed.'}
        trades = op.get('tradesInfo', {}).get('trades', [])
        if use_execution_time and row['time_quality'] == 'matched_date_only' and 0 < len(trades) <= 10000:
            if (all(trade.get('num') and Decimal(str(trade.get('quantity','0'))) > 0
                    and amount(trade.get('price'), currency) > 0 for trade in trades)
                    and len({trade['num'] for trade in trades}) == len(trades)
                    and len({trade['date'] for trade in trades}) == 1
                    and sum((Decimal(str(trade['quantity'])) for trade in trades), Decimal(0)) == Decimal(str(op['quantityDone']))
                    and sum((Decimal(str(trade['quantity'])) * amount(trade['price'], currency) for trade in trades), Decimal(0))
                        == Decimal(str(op['quantityDone'])) * amount(op['price'], currency)):
                evidence[row['record']]['executions'] = [{'id':trade['num'], 'date':trade['date'],
                                                        'quantity':str(trade['quantity']), 'price':exact(amount(trade['price'], currency))} for trade in trades]
                evidence[row['record']]['use_execution_time'] = True
    return {'source_sha256': source['audit']['sha256'], 'trade_evidence': evidence}


def income_evidence_mapping(source, matches, report, snapshot_hashes):
    evidence = {}
    def operation(raw):
        currency = raw['payment']['currency'].upper()
        return {'id': raw['id'], 'account_id': raw['brokerAccountId'], 'type': raw['type'],
                'state': raw['state'], 'ticker': raw['ticker'], 'currency': currency,
                'payment': exact(amount(raw['payment'], currency)), 'date': raw['date']}
    for row in report['rows']:
        if row['status'] not in {'income_amounts_reconciled', 'income_payment_reconciled_tax_pending'}:
            continue
        credit = matches['broker_operations'][row['broker_operation_id']]['raw']
        tax = matches['broker_operations'][row['linked_fee_tax_operations'][0]]['raw'] if row['linked_fee_tax_operations'] else None
        evidence[row['record']] = {'source_row_id': row['source_row_id'],
                                  'snapshot_sha256': snapshot_hashes[credit['brokerAccountId']],
                                  'report_hash': report['hash'], 'instrument_uid': credit['instrumentUid'],
                                  'figi':credit.get('figi',''), 'position_uid':credit.get('positionUid',''),
                                  'reason': 'Gross broker credit and distinct withholding reconciled with source Quantity and FeeTax.',
                                  'credit': operation(credit), 'tax': operation(tax) if tax else {},
                                  'tax_review_required': tax is None, 'source_price_metadata':row.get('facts',{}).get('source_price_metadata','')}
    return {'source_sha256': source['audit']['sha256'], 'income_evidence': evidence}


def cash_evidence_mapping(source, matches, report, snapshot_hashes):
    evidence = {}
    for row in report['rows']:
        if row['status'] != 'cash_amounts_reconciled':
            continue
        op = matches['broker_operations'][row['broker_operation_id']]['raw']
        currency = op['payment']['currency'].upper()
        evidence[row['record']] = {'source_row_id': row['source_row_id'],
            'snapshot_sha256': snapshot_hashes[op['brokerAccountId']], 'report_hash': report['hash'],
            'figi':op.get('figi',''), 'position_uid':op.get('positionUid',''),
            'instrument_uid': op.get('instrumentUid',''), 'instrument_type': op.get('instrumentType',''),
            'reason': 'Observed broker cash payment reconciled with source components; separate taxes and full history still require review.',
            'operation': {'id':op['id'], 'account_id':op['brokerAccountId'], 'type':op['type'], 'state':op['state'],
                          'ticker':op.get('ticker',''), 'currency':currency, 'payment':exact(amount(op['payment'],currency)), 'date':op['date']}}
    return {'source_sha256':source['audit']['sha256'], 'cash_evidence':evidence}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=Path, required=True)
    parser.add_argument('--broker-dir', type=Path, required=True)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--evidence-out', type=Path, help='optional private evidence for the financial preparation API')
    parser.add_argument('--income-evidence-out', type=Path, help='optional private income evidence for financial preparation API')
    parser.add_argument('--cash-evidence-out', type=Path, help='optional private cash evidence for preparation API')
    parser.add_argument('--include-observed-income', action='store_true', help='prepare zero-withholding payments with an explicit incomplete tax review flag')
    parser.add_argument('--use-execution-time', action='store_true', help='restore date-only source times from complete simultaneous broker executions')
    parser.add_argument('--retain-income-price-metadata', action='store_true', help='use verified cash for income while retaining nonzero source Price as uninterpreted metadata')
    args = parser.parse_args()
    source = read_verified_source(args.source)
    paths = sorted(args.broker_dir.glob('account-*.json'))
    if not paths:
        raise ValueError('no account snapshots')
    snapshots = [json.loads(path.read_text()) for path in paths]
    with localcontext() as ctx:
        ctx.prec = 1024
        matches = reconcile(source, snapshots)
        result = financial_evidence(source, matches, args.include_observed_income, args.retain_income_price_metadata)
    result['inputs'] = {path.name: hashlib.sha256(path.read_bytes()).hexdigest() for path in [args.source, *paths]}
    result['hash'] = hashlib.sha256(json.dumps(result, sort_keys=True, ensure_ascii=False).encode()).hexdigest()
    fd = os.open(args.out, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as output:
        json.dump(result, output, ensure_ascii=False, indent=2)
    if args.evidence_out:
        hashes = {snapshot['account']['id']: hashlib.sha256(path.read_bytes()).hexdigest() for snapshot, path in zip(snapshots, paths)}
        with localcontext() as ctx:
            ctx.prec = 1024
            evidence = evidence_mapping(source, matches, result, hashes, args.use_execution_time)
        fd = os.open(args.evidence_out, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, 'w') as output:
            json.dump(evidence, output, ensure_ascii=False, indent=2)
    if args.income_evidence_out:
        hashes = {snapshot['account']['id']: hashlib.sha256(path.read_bytes()).hexdigest() for snapshot, path in zip(snapshots, paths)}
        with localcontext() as ctx:
            ctx.prec = 1024
            evidence = income_evidence_mapping(source, matches, result, hashes)
        fd = os.open(args.income_evidence_out, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, 'w') as output:
            json.dump(evidence, output, ensure_ascii=False, indent=2)
    if args.cash_evidence_out:
        hashes = {snapshot['account']['id']: hashlib.sha256(path.read_bytes()).hexdigest() for snapshot, path in zip(snapshots, paths)}
        with localcontext() as ctx:
            ctx.prec = 1024
            evidence = cash_evidence_mapping(source, matches, result, hashes)
        fd = os.open(args.cash_evidence_out, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, 'w') as output:
            json.dump(evidence, output, ensure_ascii=False, indent=2)
    print(json.dumps(result['summary'], ensure_ascii=False))

if __name__ == '__main__':
    main()
