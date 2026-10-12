"""Offline comparison of rehearsal heads with recorded T-Invest snapshots.

An observed match is never proof of complete history or permission for cutover.
"""
import argparse
from datetime import datetime
from decimal import Decimal, localcontext
import hashlib
import json
import os
import re
from pathlib import Path


def number(value):
    if isinstance(value, (float, bool)):
        raise ValueError('exact decimal string or integer required')
    result = Decimal(str(value))
    if not result.is_finite():
        raise ValueError('finite decimal required')
    return result


def money(value):
    units, nano = number(value['units']), number(value.get('nano', 0))
    if units < -9223372036854775808 or units > 9223372036854775807 or units != units.to_integral_value() or nano != nano.to_integral_value() or abs(nano) >= 1000000000 or units * nano < 0:
        raise ValueError('invalid MoneyValue')
    return units + nano / Decimal(1000000000)


def timestamp(value):
    match = re.fullmatch(r'(.*:\d{2})(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})', value)
    if not match:
        raise ValueError('explicit RFC3339 timezone required')
    whole, fraction, zone = match.groups()
    result = datetime.fromisoformat(whole + zone.replace('Z', '+00:00'))
    return result, (fraction or '').ljust(9, '0')


def compare(rehearsal, brokers, identities):
    accounts = {}
    for broker in brokers:
        account = broker['account']['id']
        if account in accounts:
            raise ValueError('duplicate broker account snapshot')
        if broker['positions'].get('accountId') != account or broker['portfolio'].get('accountId') != account:
            raise ValueError('broker snapshot account mismatch')
        accounts[account] = broker
    out = {'version': 1, 'state': 'comparison_only', 'cutover_allowed': False,
           'source_sha256': rehearsal['SourceSHA256'], 'accounts': [],
           'limitations': ['Observed snapshot comparison, not closing broker-statement reconciliation.',
                           'No missing operations, costs, balances or corporate terms are invented.',
                           'Only accounts actually posted in the isolated rehearsal can be compared.']}
    with localcontext() as ctx:
        ctx.prec = 1024
        for source in rehearsal['Accounts']:
            item = {'source_account': source['SourceAccount'], 'state': 'not_compared',
                    'issues': [], 'positions': [], 'cash': []}
            out['accounts'].append(item)
            broker = accounts.get(source['SourceAccount'].removeprefix('tinvest:'))
            ledger = source.get('Snapshot')
            if not broker or not ledger:
                item['issues'].append('broker_snapshot_or_posted_ledger_missing')
                continue
            item['ledger_as_of'], item['broker_observed_at'] = ledger['AsOf'], broker['fetched_at']
            if timestamp(ledger['AsOf']) != timestamp(broker['fetched_at']):
                item['issues'].append('as_of_mismatch')
            positions = broker['positions']
            if positions.get('limitsLoadingInProgress'):
                item['issues'].append('broker_limits_loading')
            if positions.get('futures') or positions.get('options') or broker['portfolio'].get('virtualPositions'):
                item['issues'].append('unsupported_or_virtual_positions')
            portfolio = {}
            for p in broker['portfolio'].get('positions', []):
                uid = p.get('instrumentUid')
                if not uid:
                    continue
                if uid in portfolio:
                    raise ValueError('duplicate portfolio security identity')
                portfolio[uid] = p
            expected = {}
            for p in positions.get('securities', []):
                uid = p['instrumentUid']
                key = 'tinvest:' + uid
                evidence = identities.get(uid)
                if evidence:
                    if evidence['position_uid'] != p['positionUid'] or evidence['figi'] != p['figi']:
                        item['issues'].append('instrument_identity_evidence_mismatch')
                        continue
                    key = 'tinvest-position:' + p['positionUid']
                if key in expected:
                    raise ValueError('duplicate broker security identity')
                total = number(p['balance']) + number(p.get('blocked', '0'))
                expected[key] = total
                view = portfolio.get(uid)
                if view and money(view['quantity']) != total:
                    item['issues'].append('broker_position_views_disagree')
            actual = {}
            for p in ledger.get('Positions') or []:
                key = p['InstrumentKey']
                if key in actual:
                    raise ValueError('duplicate ledger security identity')
                actual[key] = number(p['Quantity'])
            for key in sorted(expected.keys() | actual.keys()):
                before, after = actual.get(key, Decimal(0)), expected.get(key, Decimal(0))
                if before == after == 0:
                    continue
                item['positions'].append({'instrument_key': key, 'ledger': str(before), 'broker_total': str(after),
                                          'difference': str(before-after), 'matches': before == after})
            cash, blocked = {}, {}
            for p in positions.get('money', []):
                ccy = p['currency'].upper()
                if ccy in cash:
                    raise ValueError('duplicate broker cash currency')
                cash[ccy] = money(p)
            for p in positions.get('blocked', []):
                ccy = p['currency'].upper()
                if ccy in blocked:
                    raise ValueError('duplicate blocked cash currency')
                blocked[ccy] = money(p)
            native = {}
            for p in ledger.get('Cash') or []:
                if p['Currency'] in native:
                    raise ValueError('duplicate ledger cash currency')
                native[p['Currency']] = p
            for ccy in sorted(cash.keys() | native.keys() | blocked.keys()):
                value = native.get(ccy, {'Settled': '0', 'Economic': '0', 'Receivable': '0', 'Payable': '0'})
                settled = number(value['Settled'])
                unambiguous = blocked.get(ccy, Decimal(0)) == 0 and number(value['Receivable']) == number(value['Payable']) == 0
                item['cash'].append({'currency': ccy, 'ledger_settled': str(settled),
                                     'ledger_economic': value['Economic'], 'broker_money': str(cash.get(ccy, Decimal(0))),
                                     'broker_blocked': str(blocked.get(ccy, Decimal(0))),
                                     'difference': str(settled-cash.get(ccy, Decimal(0))) if unambiguous else None,
                                     'matches': settled == cash.get(ccy, Decimal(0)) if unambiguous else None})
                if not unambiguous:
                    item['issues'].append('cash_settlement_or_blocking_basis_unconfirmed')
            item['issues'] = sorted(set(item['issues']))
            item['state'] = 'observed_comparison'
    out['summary'] = {'compared_accounts': sum(a['state'] == 'observed_comparison' for a in out['accounts']),
                      'position_mismatches': sum(not p['matches'] for a in out['accounts'] for p in a['positions']),
                      'cash_mismatches': sum(p['matches'] is False for a in out['accounts'] for p in a['cash'])}
    return out


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--rehearsal', required=True)
    parser.add_argument('--mapping', required=True)
    parser.add_argument('--broker', action='append', required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    hashes = {}
    def read(path):
        raw = Path(path).read_bytes()
        hashes[str(Path(path).resolve())] = hashlib.sha256(raw).hexdigest()
        return json.loads(raw, parse_float=Decimal)
    rehearsal, mapping = read(args.rehearsal), read(args.mapping)
    if rehearsal['SourceSHA256'] != mapping['source_sha256']:
        raise ValueError('mapping source checksum mismatch')
    report = compare(rehearsal, [read(p) for p in args.broker], mapping.get('instrument_identities', {}))
    report['inputs'] = hashes
    fd = os.open(args.output, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, 'w') as target:
        os.fchmod(target.fileno(), 0o600)
        json.dump(report, target, indent=2)
    print(json.dumps(report['summary']))


if __name__ == '__main__':
    main()
