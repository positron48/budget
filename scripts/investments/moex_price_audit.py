#!/usr/bin/env python3
"""Read-only personal research: compare exact-ISIN ISS daily results with T-Invest.

No market source registration, unadjusted-price approval or financial posting.
ISS access is not a licence to redistribute data or operate a production feed.
"""
import argparse
from datetime import datetime, timedelta, timezone
from decimal import Decimal, localcontext
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import time
import urllib.parse
import urllib.request

BASE = 'https://iss.moex.com/iss/'
LIMIT = 16 * 1024 * 1024


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def encode(value):
    return json.dumps(value, ensure_ascii=False, separators=(',', ':'), allow_nan=False).encode()


def private_read(path, expected=None):
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_mode & 0o077 or info.st_size > LIMIT:
        raise ValueError('invalid private evidence file')
    raw = path.read_bytes()
    if expected is not None and digest(raw) != expected:
        raise ValueError('evidence checksum mismatch')
    return raw


def publish(path, value):
    raw = encode(value)
    # Atomic immutable publication; a crash cannot leave a partial checkpoint.
    temporary = path.parent / ('.tmp-' + os.urandom(12).hex())
    fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    try:
        with os.fdopen(fd, 'wb') as stream:
            stream.write(raw)
            stream.flush()
            os.fsync(stream.fileno())
        try:
            os.link(temporary, path)
        except FileExistsError:
            if private_read(path) != raw:
                raise ValueError('immutable evidence changed')
    finally:
        temporary.unlink(missing_ok=True)


def table(body, key):
    value = body.get(key)
    if not isinstance(value, dict):
        raise ValueError('missing ISS table')
    columns, rows = value.get('columns'), value.get('data')
    if (not isinstance(columns, list) or not columns or len(set(columns)) != len(columns)
            or not all(isinstance(c, str) for c in columns) or not isinstance(rows, list)
            or len(rows) > 1000):
        raise ValueError('invalid ISS table')
    if any(not isinstance(row, list) or len(row) != len(columns) for row in rows):
        raise ValueError('invalid ISS row')
    return [dict(zip(columns, row)) for row in rows]


def number(value):
    if isinstance(value, bool) or not isinstance(value, (str, int, Decimal)):
        raise ValueError('invalid exact price')
    n = Decimal(value)
    if not n.is_finite() or n < 0 or len(str(value)) > 100:
        raise ValueError('invalid exact price')
    return n


def text_number(value):
    return format(number(value), 'f')


def quotation(value):
    units, nano = value.get('units', '0'), value.get('nano', 0)
    if isinstance(units, int) and not isinstance(units, bool):
        units = str(units)
    if not isinstance(units, str):
        raise ValueError('invalid T-Invest units')
    if (not re.fullmatch(r'-?[0-9]{1,24}', units) or isinstance(nano, bool)
            or not isinstance(nano, int) or abs(nano) >= 10**9):
        raise ValueError('invalid T-Invest quotation')
    with localcontext() as context:
        context.prec = 128
        return text_number(Decimal(units) + Decimal(nano) / Decimal(10**9))


def instant(value):
    at = datetime.fromisoformat(value.replace('Z', '+00:00'))
    if at.utcoffset() is None:
        raise ValueError('timezone required')
    return at.astimezone(timezone.utc)


def plan(history, financial_report):
    coverage_raw = private_read(history / 'coverage.json')
    coverage = json.loads(coverage_raw)
    private_read(history / 'manifest.json', coverage['PlanSHA256'])
    catalog, hashes = {}, {}
    for item in coverage['Instruments']:
        if item['State'] != 'observed':
            continue
        instrument = item['Instrument']
        private_read(history / (instrument['id'] + '-metadata.json'), item['MetadataSHA256'])
        catalog[instrument['id']] = instrument
        for h in item['CheckpointSHA256']:
            if h in hashes and hashes[h] != instrument['id']:
                raise ValueError('ambiguous checkpoint identity')
            hashes[h] = instrument['id']
    candles = {}
    for path in sorted(history.glob('*.json')):
        if not re.fullmatch(r'[a-f0-9]{64}\.json', path.name):
            continue
        raw = private_read(path)
        h = digest(raw)
        if h not in hashes:
            raise ValueError('unindexed checkpoint')
        source = json.loads(raw)
        uid = source['Request']['InstrumentID']
        if uid != hashes.pop(h) or source['Request']['Kind'] != 'daily_candle':
            raise ValueError('checkpoint identity changed')
        for candle in source['History']['candles'] or []:
            if candle.get('candleSource') != 'CANDLE_SOURCE_EXCHANGE':
                raise ValueError('non-exchange candle')
            at = instant(candle['time'])
            if at != at.replace(hour=0, minute=0, second=0, microsecond=0):
                raise ValueError('daily UTC source identity required')
            if not isinstance(candle.get('isComplete'), bool):
                raise ValueError('explicit completeness required')
            key = uid, candle['time']
            if key in candles and candles[key] != candle:
                raise ValueError('conflicting overlap')
            candles[key] = candle
    if hashes:
        raise ValueError('missing checkpoint')
    report_raw = private_read(financial_report)
    report = json.loads(report_raw)
    entries = {}
    for account in report['Accounts']:
        for point in account.get('Checkpoints', []):
            date = instant(point['AsOf']).astimezone(timezone(timedelta(hours=3))).date()
            cutoff = datetime.combine(date, datetime.max.time(), timezone(timedelta(hours=3)))
            for position in point['Positions']:
                if Decimal(position['Quantity']) == 0:
                    continue
                if not position['InstrumentKey'].startswith('tinvest:'):
                    raise ValueError('explicit broker UID required')
                uid = position['InstrumentKey'].removeprefix('tinvest:')
                instrument = catalog[uid]
                if not re.fullmatch(r'[A-Z]{2}[A-Z0-9]{9}[0-9]', instrument['isin']):
                    raise ValueError('exact ISIN required')
                eligible = [c for (identity, _), c in candles.items() if identity == uid
                            and c['isComplete'] and instant(c['time']) + timedelta(days=1) <= cutoff]
                if not eligible:
                    raise ValueError('missing eligible source price')
                selected = max(eligible, key=lambda c: instant(c['time']))
                if cutoff >= instant(selected['time']) + timedelta(days=5):
                    raise ValueError('source price expired')
                entry = {'UID': uid, 'ISIN': instrument['isin'], 'Kind': instrument['kind'],
                         'ValuationDate': str(date), 'ObservationDate': selected['time'][:10],
                         'TInvestClose': quotation(selected['close'])}
                key = uid, str(date)
                if key in entries and entries[key] != entry:
                    raise ValueError('ambiguous checkpoint valuation')
                entries[key] = entry
    if not entries or len(entries) > 1000:
        raise ValueError('invalid research universe')
    return {'Version': 1, 'Use': 'personal_research_only', 'HistoryCoverageSHA256': digest(coverage_raw),
            'FinancialReportSHA256': digest(report_raw), 'Entries': [entries[k] for k in sorted(entries)]}


class Evidence:
    def __init__(self, directory, offline=False, transport=None):
        self.directory, self.offline = directory, offline
        self.transport = transport or self.fetch
        self.next_request = 0
        self.references = {}

    def fetch(self, url):
        delay = self.next_request - time.monotonic()
        if delay > 0:
            time.sleep(delay)
        self.next_request = time.monotonic() + 1
        request = urllib.request.Request(url, headers={'User-Agent': 'Budget-personal-price-research/1'})
        with urllib.request.urlopen(request, timeout=30) as response:
            if response.geturl() != url:
                raise ValueError('ISS redirect refused')
            raw = response.read(LIMIT + 1)
        if len(raw) > LIMIT:
            raise ValueError('ISS response too large')
        return raw

    def get(self, path, parameters):
        if not re.fullmatch(r'[A-Za-z0-9/_-]+\.json', path):
            raise ValueError('invalid ISS path')
        url = BASE + path + '?' + urllib.parse.urlencode(parameters)
        checkpoint = self.directory / (digest(url.encode()) + '.json')
        if not checkpoint.exists():
            if self.offline:
                raise ValueError('missing offline ISS checkpoint')
            raw = self.transport(url)
            json.loads(raw, parse_float=Decimal)  # reject invalid JSON before publication
            publish(checkpoint, {'URL': url, 'RetrievedAt': datetime.now(timezone.utc).isoformat(),
                                 'BodySHA256': digest(raw), 'Body': raw.decode('utf-8')})
        saved = json.loads(private_read(checkpoint))
        if saved['URL'] != url or digest(saved['Body'].encode()) != saved['BodySHA256']:
            raise ValueError('ISS checkpoint identity/checksum changed')
        self.references[checkpoint.name] = digest(private_read(checkpoint))
        return json.loads(saved['Body'], parse_float=Decimal)


def compare(entry, evidence):
    metadata = evidence.get('securities.json', {'q': entry['ISIN'], 'iss.meta': 'off'})
    market = 'bonds' if entry['Kind'] == 'bond' else 'shares'
    groups = {'bond': {'stock_bonds'}, 'share': {'stock_shares'},
              'etf': {'stock_etf', 'stock_ppif'}}[entry['Kind']]
    found = []
    candidates = []
    for security in table(metadata, 'securities'):
        if security.get('isin') != entry['ISIN'] or security.get('group') not in groups:
            continue
        secid, board = security.get('secid'), security.get('primary_boardid')
        if (not isinstance(secid, str) or not re.fullmatch(r'[A-Za-z0-9_-]{1,64}', secid)
                or not isinstance(board, str) or not re.fullmatch(r'[A-Z0-9]{4}', board)):
            continue
        identity = secid, board
        if identity in candidates:
            continue
        candidates.append(identity)
        payload = evidence.get(f'history/engines/stock/markets/{market}/boards/{board}/securities/{secid}.json',
                               {'from': entry['ObservationDate'], 'till': entry['ObservationDate'], 'iss.meta': 'off'})
        for row in table(payload, 'history'):
            if row.get('SECID') != secid or row.get('BOARDID') != board or row.get('TRADEDATE') != entry['ObservationDate']:
                raise ValueError('history row identity mismatch')
            if row.get('CLOSE') is None:
                continue
            close, low, high = number(row['CLOSE']), number(row['LOW']), number(row['HIGH'])
            if not low <= close <= high or row.get('CURRENCYID') not in {'SUR', 'RUB'}:
                raise ValueError('invalid history price/currency')
            found.append({'SECID': secid, 'Board': board, 'Close': text_number(close),
                          'Low': text_number(low), 'High': text_number(high),
                          'CurrencyID': row['CURRENCYID'], 'TradingSession': row.get('TRADINGSESSION')})
    out = dict(entry, Candidates=len(candidates), ApprovedForNAV=False)
    if len(found) != 1:
        return dict(out, State='history_missing' if not found else 'ambiguous_history', Rows=found)
    row = found[0]
    source, close = number(entry['TInvestClose']), number(row['Close'])
    if source == 0 or close == 0:
        return dict(out, State='zero_price_diagnostic', MOEX=row)
    ratio = close / source
    # Ratios are diagnostics only. No approximate comparison approves a quote.
    factor = next((v for v in ('1', '10', '100', '0.0002') if abs(ratio / Decimal(v) - 1) <= Decimal('.02')), None)
    return dict(out, State='compared', MOEX=row, ExactCloseMatch=source == close,
                CloseRatio=text_number(ratio), NearCandidateMultiplier=factor)


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--history-dir', type=Path, required=True)
    p.add_argument('--financial-report', type=Path, required=True)
    p.add_argument('--out', type=Path, required=True)
    p.add_argument('--offline', action='store_true')
    args = p.parse_args()
    if not args.offline:
        args.out.mkdir(mode=0o700, parents=True, exist_ok=True)
    info = args.out.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_mode & 0o077:
        raise ValueError('private research directory required')
    manifest = plan(args.history_dir, args.financial_report)
    path = args.out / 'manifest.json'
    if path.exists():
        if json.loads(private_read(path)) != manifest:
            raise ValueError('research plan changed; use new directory')
    elif args.offline:
        raise ValueError('missing offline manifest')
    else:
        publish(path, manifest)
    evidence = Evidence(args.out, args.offline)
    rows = []
    for entry in manifest['Entries']:
        row = compare(entry, evidence)
        rows.append(row)
        print(f"instrument={entry['UID']} date={entry['ValuationDate']} state={row['State']} approved_for_nav=false", flush=True)
    report = {'Version': 1, 'ManifestSHA256': digest(private_read(path)), 'ApprovedForNAV': False, 'Requests': dict(sorted(evidence.references.items())), 'Rows': rows}
    result = args.out / 'audit.json'
    if result.exists():
        if json.loads(private_read(result)) != report:
            raise ValueError('research response changed')
    elif args.offline:
        raise ValueError('missing offline audit index')
    else:
        publish(result, report)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        # Do not print external bodies, request exception details or financial values.
        raise SystemExit('MOEX research failed: ' + type(error).__name__) from None
