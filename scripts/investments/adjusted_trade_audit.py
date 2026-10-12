#!/usr/bin/env python3
"""Find economically matching transformed trades for review, never for posting."""
import argparse
from collections import Counter
from decimal import Decimal, localcontext, ROUND_HALF_UP
import hashlib
import json
import os
from pathlib import Path
from financial_evidence import amount, exact
from reconcile import read_verified_source, reconcile, walltime


def candidate(row, op):
    raw, dec = row['raw'], row['decimals']
    kinds = {'BUY': {'OPERATION_TYPE_BUY', 'OPERATION_TYPE_BUY_CARD'},
             'SELL': {'OPERATION_TYPE_SELL', 'OPERATION_TYPE_SELL_CARD'}}
    if (op.get('state') != 'OPERATION_STATE_EXECUTED' or op.get('type') not in kinds.get(raw['Event'], set())
            or op.get('instrumentType') not in {'share', 'etf'}
            or raw['Date'] != walltime(op['date']) or raw.get('DoNotAdjustCash') != 'False'):
        return None
    currency = raw['Currency'].upper()
    with localcontext() as ctx:
        ctx.prec = 1024
        try:
            qty, price = Decimal(dec['Quantity']), Decimal(dec['Price'])
            original_qty = Decimal(op['quantityDone'])
            original_price = amount(op['price'], currency)
            payment = amount(op['payment'], currency)
            fee = amount(op['commission'], currency)
            if (not all(v.is_finite() for v in (qty, price, original_qty))
                    or min(qty, price, original_qty, original_price) <= 0
                    or qty == original_qty or Decimal(dec.get('NKD') or '0') != 0
                    or fee > 0 or -fee != Decimal(dec['FeeTax'])
                    or (raw.get('FeeCurrency') or currency).upper() != currency
                    or (payment >= 0 if raw['Event'] == 'BUY' else payment <= 0)):
                return None
            cash = abs(payment)
            if any(v.quantize(Decimal('.01'), rounding=ROUND_HALF_UP) != cash
                   for v in (qty * price, original_qty * original_price)):
                return None
            return {'source_row_id': row['id'], 'record': row['record'],
                    'source_ticker': raw['Symbol'], 'broker_ticker': op.get('ticker'),
                    'broker_account_id': op['brokerAccountId'], 'broker_operation_id': op['id'],
                    'instrument_uid': op['instrumentUid'], 'date': op['date'],
                    'source_quantity': exact(qty), 'broker_quantity': exact(original_qty),
                    'observed_quantity_ratio': exact(qty / original_qty),
                    'cash': exact(cash), 'currency': currency}
        except (ValueError, KeyError, ArithmeticError):
            return None


def audit(source, matches):
    used = {r['broker_operation_id'] for r in matches['rows'] if r['status'] == 'matched'}
    unmatched = {r['source_record'] for r in matches['rows'] if r['status'] != 'matched'}
    rows = []
    for row in source['rows']:
        if row['record'] not in unmatched:
            continue
        for identity, entry in matches['broker_operations'].items():
            if identity in used:
                continue
            found = candidate(row, entry['raw'])
            if found:
                rows.append(found)
    sources = Counter(r['source_row_id'] for r in rows)
    brokers = Counter((r['broker_account_id'], r['broker_operation_id']) for r in rows)
    for row in rows:
        row['status'] = ('corporate_action_review_required' if sources[row['source_row_id']] == 1
                         and brokers[(row['broker_account_id'], row['broker_operation_id'])] == 1
                         else 'ambiguous')
    return {'source_sha256': source['audit']['sha256'], 'candidates': rows,
            'posting_allowed': False,
            'limitations': ['cash equality is not proof of instrument succession',
                            'quantity ratio is observational, not a confirmed corporate action',
                            'cost allocation, fractional entitlements and effective date require evidence']}


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--source', type=Path, required=True)
    p.add_argument('--broker-dir', type=Path, required=True)
    p.add_argument('--out', type=Path, required=True)
    args = p.parse_args()
    source = read_verified_source(args.source)
    paths = sorted(args.broker_dir.glob('account-*.json'))
    if not paths:
        raise ValueError('no account snapshots')
    with localcontext() as ctx:
        ctx.prec = 1024
        result = audit(source, reconcile(source, [json.loads(p.read_text()) for p in paths]))
    result['inputs'] = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in [args.source, *paths]}
    result['hash'] = hashlib.sha256(json.dumps(result, sort_keys=True).encode()).hexdigest()
    with os.fdopen(os.open(args.out, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as f:
        json.dump(result, f, ensure_ascii=False, indent=2)
    print(json.dumps(dict(Counter(r['status'] for r in result['candidates']))))


if __name__ == '__main__':
    main()
