#!/usr/bin/env python3
"""Lossless, conservative Snowball/T-Invest account reconciliation (no posting)."""
import argparse
from collections import Counter, defaultdict
from datetime import datetime
from decimal import Decimal, localcontext, ROUND_HALF_UP
import hashlib
import json
from pathlib import Path
from zoneinfo import ZoneInfo

MOSCOW = ZoneInfo('Europe/Moscow')
EVENTS = {
    'BUY': {'OPERATION_TYPE_BUY', 'OPERATION_TYPE_BUY_CARD'},
    'SELL': {'OPERATION_TYPE_SELL', 'OPERATION_TYPE_SELL_CARD'},
    'DIVIDEND': {'OPERATION_TYPE_DIVIDEND', 'OPERATION_TYPE_COUPON'},
    'REPAYMENT': {'OPERATION_TYPE_BOND_REPAYMENT_FULL', 'OPERATION_TYPE_BOND_REPAYMENT'},
    'CASH_IN': {'OPERATION_TYPE_INPUT'},
    'TAX': {'OPERATION_TYPE_TAX'},
    'FEE': {'OPERATION_TYPE_SERVICE_FEE', 'OPERATION_TYPE_OTHER_FEE'},
    'CASH_GAIN': {'OPERATION_TYPE_OVERNIGHT'},
}
SECURITY_EVENTS = {'BUY', 'SELL', 'DIVIDEND', 'REPAYMENT'}


def money(value):
    value = value or {}
    return Decimal(value.get('units', '0')) + Decimal(value.get('nano', 0)) / Decimal(10**9)


def walltime(value):
    return datetime.fromisoformat(value.replace('Z', '+00:00')).astimezone(MOSCOW).strftime('%Y-%m-%d %H:%M:%S')


def trade_price_matches(row, op):
    with localcontext() as ctx:
        ctx.prec = 1024
        price = Decimal(row['decimals']['Price'])
        quoted = money(op.get('price'))
        if price == quoted:
            return True
        quantity = Decimal(row['decimals']['Quantity'])
        gross = abs(money(op.get('payment')))
        scale = max(0, -price.as_tuple().exponent)
        if (op.get('instrumentType') not in {'share', 'etf'}
                or Decimal(row['decimals'].get('NKD') or '0') != 0
                or price <= 0 or quoted <= 0 or quantity <= 0 or not 6 <= scale <= 12
                or gross != gross.quantize(Decimal('.01'))):
            return False
        average = gross / quantity
        return (quoted == average.quantize(Decimal('.000001'), rounding=ROUND_HALF_UP)
                and price == average.quantize(Decimal(1).scaleb(-scale), rounding=ROUND_HALF_UP))


def economics(row, op):
    """Exact economics or two rounded average prices derived from independent cash."""
    raw, dec = row['raw'], row['decimals']
    event = raw['Event']
    if op['type'] not in EVENTS.get(event, set()):
        return False
    if event in SECURITY_EVENTS and raw['Symbol'] != op.get('ticker'):
        return False
    if raw['Currency'].upper() != op.get('payment', {}).get('currency', '').upper():
        return False
    if event in {'BUY', 'SELL'}:
        commission = op.get('commission')
        # A missing fee is unknown, not a proven zero fee.
        if commission is None or abs(money(commission)) != Decimal(dec['FeeTax']):
            return False
        fee_currency = raw['FeeCurrency'] or raw['Currency']
        if commission.get('currency', '').upper() != fee_currency.upper():
            return False
        return (Decimal(dec['Quantity']) == Decimal(op.get('quantityDone', '0'))
                and trade_price_matches(row, op))
    if event in {'DIVIDEND', 'CASH_IN', 'CASH_GAIN'}:
        return Decimal(dec['Quantity']) == money(op.get('payment'))
    if event == 'REPAYMENT':
        return Decimal(dec['Quantity']) * Decimal(dec['Price']) == money(op.get('payment'))
    if event in {'FEE', 'TAX'}:
        return Decimal(dec['FeeTax']) == -money(op.get('payment'))
    return False


def reconcile(source, snapshots):
    if source.get('scope') != 'combined_unassigned' or source.get('version') != 1:
        raise ValueError('unsupported source stage')
    accounts = []
    operations = {}
    by_day = defaultdict(list)
    for snapshot in snapshots:
        a = snapshot['account']
        key = 'tinvest:' + a['id']
        if any(x['id'] == key for x in accounts):
            raise ValueError('duplicate account snapshot')
        label = {'ACCOUNT_TYPE_TINKOFF': 'Т-Банк — обычный', 'ACCOUNT_TYPE_TINKOFF_IIS': 'Т-Банк — ИИС'}.get(a['type'])
        if not label:
            raise ValueError('unexpected account type; explicit account mapping required')
        accounts.append({'id': key, 'name': label, 'source': 'tinvest', 'broker_account_id': a['id']})
        for op in snapshot['operations']:
            identity = key + ':' + op['id']
            if identity in operations:
                if operations[identity]['raw'] != op:
                    raise ValueError('conflicting broker operation identity')
                continue
            if op.get('brokerAccountId') != a['id']:
                raise ValueError('operation belongs to another account')
            entry = {'account_id': key, 'raw': op, 'wall_time': walltime(op['date'])}
            operations[identity] = entry
            if op['state'] == 'OPERATION_STATE_EXECUTED':
                by_day[entry['wall_time'][:10]].append(identity)
    accounts.append({'id': 'other:unassigned', 'name': 'ВТБ и Альфа — не распределено', 'source': 'snowball',
                     'provisional': True, 'includes': ['ВТБ — заблокированные акции', 'ВТБ — обычный', 'Альфа — ИИС', 'Альфа — обычный']})
    candidates = {}
    potential = {}
    references = Counter()
    for row in source['rows']:
        exact, near = [], []
        for identity in by_day[row['raw']['Date'][:10]]:
            op = operations[identity]
            if economics(row, op['raw']):
                if row['raw']['Date'] == op['wall_time']:
                    exact.append(identity)
                else:
                    near.append(identity)
        candidates[row['id']] = exact
        potential[row['id']] = near
        # Midnight source rows may be date-only. Preserve the assumption and
        # accept only a unique economic match on both sides across the whole file.
        chosen = exact or (near if row['raw']['Date'].endswith('00:00:00') else [])
        references.update(chosen)
    rows = []
    matched = set()
    for row in source['rows']:
        exact = candidates[row['id']]
        date_only = not exact and row['raw']['Date'].endswith('00:00:00')
        chosen = potential[row['id']] if date_only else exact
        accepted = len(chosen) == 1 and references[chosen[0]] == 1
        record = {'source_row_id': row['id'], 'source_record': row['record'], 'account_id': 'other:unassigned',
                  'status': 'unmatched', 'exact_candidates': exact, 'same_day_candidates': potential[row['id']]}
        if accepted:
            identity = chosen[0]
            matched.add(identity)
            record.update(account_id=operations[identity]['account_id'], status='matched_date_only' if date_only else 'matched', broker_operation_id=identity,
                          evidence='unique_type_symbol_currency_day_and_exact_economics' if date_only else 'unique_type_symbol_currency_second_and_exact_economics',
                          financial_validation='pending')
        elif exact:
            record['status'] = 'ambiguous'
        elif potential[row['id']]:
            record['status'] = 'same_day_review'
        rows.append(record)
    # Child broker commissions are evidence for a trade fee, not a second
    # expense to append. Taxes without parent IDs need their own exact link.
    components = {}
    component_candidates = {}
    component_references = Counter()
    for row, mapping in zip(source['rows'], rows):
        parent = mapping.get('broker_operation_id')
        if not parent:
            continue
        broker = operations[parent]
        linked = []
        for identity, entry in operations.items():
            op = entry['raw']
            if identity in matched or entry['account_id'] != broker['account_id'] or op['state'] != 'OPERATION_STATE_EXECUTED':
                continue
            fee = row['raw']['Event'] in {'BUY', 'SELL'} and op['type'] == 'OPERATION_TYPE_BROKER_FEE' and op.get('parentOperationId') == broker['raw']['id']
            tax = row['raw']['Event'] == 'DIVIDEND' and op['type'] in {'OPERATION_TYPE_DIVIDEND_TAX', 'OPERATION_TYPE_BOND_TAX'} and op.get('ticker') == row['raw']['Symbol'] and entry['wall_time'] == broker['wall_time']
            if (fee or tax) and -money(op.get('payment')) == Decimal(row['decimals']['FeeTax']) and op.get('payment', {}).get('currency', '').upper() == (row['raw']['FeeCurrency'] or row['raw']['Currency']).upper():
                linked.append(identity)
        component_candidates[row['id']] = linked
        component_references.update(linked)
    # A component must be unique in both directions. First-row-wins would
    # arbitrarily allocate equal withholding to one of two same-time payouts.
    for identity, linked in component_candidates.items():
        if len(linked) == 1 and component_references[linked[0]] == 1:
            components[identity] = linked
    used_components = {identity for values in components.values() for identity in values}
    result = {'version': 1, 'state': 'account_mapping_only', 'source_sha256': source['audit']['sha256'],
              'timezone_for_matching': 'Europe/Moscow', 'accounts': accounts, 'rows': rows,
              'broker_operations': operations, 'matched_broker_operations': sorted(matched),
              'linked_fee_tax_operations': components,
              'unmatched_broker_operations': sorted(set(operations) - matched - used_components),
              'summary': {'rows': len(rows), 'by_account': dict(Counter(r['account_id'] for r in rows)),
                          'by_status': dict(Counter(r['status'] for r in rows))},
              'limitations': ['Unmatched does not prove VTB/Alfa origin; account is provisional.',
                              'API history may be incomplete after corporate actions.',
                              'Broker-only operations are retained, not added to Snowball a second time.',
                              'Matching assigns accounts only; fees, taxes, NKD and settlement still require ledger mapping.',
                              'Timezone is a matching hypothesis confirmed by exact pairs, not a rewrite of source times.']}
    result['hash'] = hashlib.sha256(json.dumps(result, sort_keys=True, ensure_ascii=False).encode()).hexdigest()
    return result


def read_verified_source(path):
    source = json.loads(path.read_text())
    import base64
    import csv
    import io
    original = base64.b64decode(source['original_csv'], validate=True)
    if hashlib.sha256(original).hexdigest() != source['audit']['sha256']:
        raise ValueError('source checksum mismatch')
    raw_rows = list(csv.DictReader(io.StringIO(original.decode('utf-8-sig'))))
    if len(raw_rows) != len(source['rows']):
        raise ValueError('source row count mismatch')
    for ordinal, (raw, staged) in enumerate(zip(raw_rows, source['rows']), 1):
        if raw != staged['raw'] or staged['id'] != f"{source['audit']['sha256']}:{ordinal}" or staged['record'] != ordinal:
            raise ValueError('staged source was modified')
        for field in ('Price', 'Quantity', 'FeeTax', 'NKD'):
            if raw[field] and Decimal(raw[field].replace(',', '.')) != Decimal(staged['decimals'][field]):
                raise ValueError('staged decimal was modified')
    return source


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=Path, required=True)
    parser.add_argument('--broker-dir', type=Path, required=True)
    parser.add_argument('--out', type=Path, required=True)
    args = parser.parse_args()
    source = read_verified_source(args.source)
    paths = sorted(args.broker_dir.glob('account-*.json'))
    if not paths:
        raise ValueError('no account snapshots')
    snapshots = [json.loads(p.read_text()) for p in paths]
    with localcontext() as ctx:
        ctx.prec = 1024
        result = reconcile(source, snapshots)
    result['inputs'] = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in [args.source, *paths]}
    result.pop('hash', None)
    result['hash'] = hashlib.sha256(json.dumps(result, sort_keys=True, ensure_ascii=False).encode()).hexdigest()
    import os
    fd = os.open(args.out, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as f:
        json.dump(result, f, ensure_ascii=False, indent=2)
    labels = {a['id']: a['name'] for a in result['accounts']}
    print(json.dumps({'by_account': {labels[k]: v for k, v in result['summary']['by_account'].items()},
                      'by_status': result['summary']['by_status'], 'broker_operations': len(result['broker_operations'])}, ensure_ascii=False))

if __name__ == '__main__':
    main()
