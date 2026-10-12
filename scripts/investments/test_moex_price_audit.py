import json
from pathlib import Path
import tempfile
import unittest
from urllib.parse import urlparse, parse_qs

from moex_price_audit import Evidence, compare, table, publish, private_read, quotation, plan, digest

ISIN = 'RU000A107UL4'
ENTRY = {'UID': '001c39ac-ebaa-4fe7-a774-f7a2ba518ef3', 'ISIN': ISIN,
         'Kind': 'share', 'ValuationDate': '2025-12-31',
         'ObservationDate': '2025-12-30', 'TInvestClose': '10.000000009'}


def block(columns, rows):
    return {'columns': columns, 'data': rows}


class Fixtures:
    def __init__(self, aliases=False, both=False, null=False, foreign=False, wrong=False):
        self.calls = 0
        self.aliases, self.both, self.null, self.foreign, self.wrong = aliases, both, null, foreign, wrong

    def __call__(self, url):
        self.calls += 1
        path = urlparse(url).path
        if path.endswith('/securities.json'):
            rows = [['T', 'OTHER' if self.foreign else ISIN, 'stock_shares', 'TQBR']]
            if self.aliases:
                rows.append(['TCSG', ISIN, 'stock_shares', 'TQBR'])
            return json.dumps({'securities': block(['secid', 'isin', 'group', 'primary_boardid'], rows)}).encode()
        secid = path.rsplit('/', 1)[1].removesuffix('.json')
        if self.aliases and secid == 'TCSG' and not self.both:
            return json.dumps({'history': block(['SECID'], [])}).encode()
        date = parse_qs(urlparse(url).query)['from'][0]
        # Raw JSON decimals stay exact; encoding a Python float here would
        # conceal the precision regression this test must exercise.
        raw = ('{"history":{"columns":["SECID","BOARDID","TRADEDATE","CLOSE","LOW","HIGH","CURRENCYID","TRADINGSESSION"],'
               '"data":[["%s","TQBR","%s",%s,10,11,"%s",3]]}}') % (
                   secid, date, 'null' if self.null else '10.000000009', 'USD' if self.wrong else 'SUR')
        return raw.encode()


class AuditTest(unittest.TestCase):
    def make_plan(self, directory, candles, as_of='2025-12-31T20:59:59Z'):
        history = Path(directory) / 'history'
        history.mkdir()
        uid = ENTRY['UID']
        instrument = {'id': uid, 'isin': ISIN, 'kind': 'share'}
        publish(history / 'manifest.json', {'version': 1})
        publish(history / (uid + '-metadata.json'), instrument)
        checkpoint = history / ('a' * 64 + '.json')
        publish(checkpoint, {'Request': {'InstrumentID': uid, 'Kind': 'daily_candle'},
                             'History': {'candles': candles}})
        publish(history / 'coverage.json', {'PlanSHA256': digest(private_read(history / 'manifest.json')),
            'Instruments': [{'State': 'observed', 'Instrument': instrument,
                'MetadataSHA256': digest(private_read(history / (uid + '-metadata.json'))),
                'CheckpointSHA256': [digest(private_read(checkpoint))]}]})
        report = Path(directory) / 'financial.json'
        publish(report, {'Accounts': [{'Checkpoints': [{'AsOf': as_of, 'Positions': [
            {'InstrumentKey': 'tinvest:' + uid, 'Quantity': '1'}]}]}]})
        return history, report

    def test_plan_uses_closed_previous_utc_candle_and_rejects_expiry(self):
        candles = [{'time': '2025-12-30T00:00:00Z', 'isComplete': True,
                    'candleSource': 'CANDLE_SOURCE_EXCHANGE', 'close': {'units': 10}},
                   {'time': '2025-12-31T00:00:00Z', 'isComplete': True,
                    'candleSource': 'CANDLE_SOURCE_EXCHANGE', 'close': {'units': 20}}]
        with tempfile.TemporaryDirectory() as directory:
            got = plan(*self.make_plan(directory, candles))
            self.assertEqual(got['Entries'][0]['ObservationDate'], '2025-12-30')
            self.assertEqual(got['Entries'][0]['TInvestClose'], '10')
        with tempfile.TemporaryDirectory() as directory:
            paths = self.make_plan(directory, candles, '2026-01-05T20:59:59Z')
            with self.assertRaises(ValueError):
                plan(*paths)

    def test_plan_requires_explicit_daily_exchange_identity(self):
        base = {'time': '2025-12-30T00:00:00Z', 'isComplete': True,
                'candleSource': 'CANDLE_SOURCE_EXCHANGE', 'close': {'units': 10}}
        for change in [{'time': '2025-12-30T12:00:00Z'}, {'isComplete': 'true'},
                       {'candleSource': 'CANDLE_SOURCE_DEALER'}]:
            with tempfile.TemporaryDirectory() as directory:
                with self.assertRaises(ValueError):
                    plan(*self.make_plan(directory, [dict(base, **change)]))
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(ValueError):
                plan(*self.make_plan(directory, None))

    def test_quotation_integer_units_and_full_precision(self):
        self.assertEqual(quotation({'units': 10, 'nano': 9}), '10.000000009')
        self.assertEqual(quotation({'units': '123456789012345678901234', 'nano': 123456789}),
                         '123456789012345678901234.123456789')
        for value in [{'units': True}, {'nano': True}, {'nano': 10**9}, {'units': 'NaN'}]:
            with self.assertRaises(ValueError):
                quotation(value)

    def test_etf_exact_isin_can_identify_exchange_traded_pif(self):
        with tempfile.TemporaryDirectory() as directory:
            fixture = Fixtures()
            def transport(url):
                return fixture(url).replace(b'stock_shares', b'stock_ppif')
            evidence = Evidence(Path(directory), transport=transport)
            self.assertEqual(compare(dict(ENTRY, Kind='etf'), evidence)['State'], 'compared')
            self.assertEqual(compare(ENTRY, evidence)['State'], 'history_missing')

    def test_resume_offline_and_precise_close(self):
        with tempfile.TemporaryDirectory() as directory:
            fixture = Fixtures()
            live = Evidence(Path(directory), transport=fixture)
            got = compare(ENTRY, live)
            self.assertEqual(got['State'], 'compared')
            self.assertTrue(got['ExactCloseMatch'])
            self.assertFalse(got['ApprovedForNAV'])
            self.assertEqual(got['MOEX']['Close'], '10.000000009')
            self.assertEqual(fixture.calls, 2)
            self.assertEqual(compare(ENTRY, live), got)
            self.assertEqual(fixture.calls, 2)
            offline = Evidence(Path(directory), offline=True, transport=lambda _: self.fail('network in offline mode'))
            self.assertEqual(compare(ENTRY, offline), got)
            self.assertEqual(live.references, offline.references)

    def test_alias_is_resolved_by_dated_history_not_ticker(self):
        with tempfile.TemporaryDirectory() as directory:
            evidence = Evidence(Path(directory), transport=Fixtures(aliases=True))
            got = compare(ENTRY, evidence)
            self.assertEqual(got['State'], 'compared')
            self.assertEqual(got['Candidates'], 2)
            self.assertEqual(got['MOEX']['SECID'], 'T')
        with tempfile.TemporaryDirectory() as directory:
            got = compare(ENTRY, Evidence(Path(directory), transport=Fixtures(aliases=True, both=True)))
            self.assertEqual(got['State'], 'ambiguous_history')
            self.assertFalse(got['ApprovedForNAV'])

    def test_foreign_isin_and_null_close_never_value_zero(self):
        for fixture in [Fixtures(foreign=True), Fixtures(null=True)]:
            with tempfile.TemporaryDirectory() as directory:
                got = compare(ENTRY, Evidence(Path(directory), transport=fixture))
                self.assertEqual(got['State'], 'history_missing')
                self.assertNotIn('MOEX', got)

    def test_conflicting_currency_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(ValueError):
                compare(ENTRY, Evidence(Path(directory), transport=Fixtures(wrong=True)))

    def test_corrupt_checkpoint_and_missing_offline_fail(self):
        with tempfile.TemporaryDirectory() as directory:
            evidence = Evidence(Path(directory), transport=Fixtures())
            compare(ENTRY, evidence)
            checkpoint = next(Path(directory).glob('*.json'))
            saved = json.loads(private_read(checkpoint))
            saved['Body'] += ' '
            checkpoint.unlink()
            publish(checkpoint, saved)
            with self.assertRaises(ValueError):
                compare(ENTRY, Evidence(Path(directory), offline=True))
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(ValueError):
                compare(ENTRY, Evidence(Path(directory), offline=True))

    def test_immutable_publication_and_private_permissions(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'evidence.json'
            publish(path, {'value': 1})
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            publish(path, {'value': 1})
            with self.assertRaises(ValueError):
                publish(path, {'value': 2})
            path.chmod(0o644)
            with self.assertRaises(ValueError):
                private_read(path)

    def test_table_shape_is_not_silently_truncated(self):
        for value in [block(['CLOSE', 'CLOSE'], [[1, 2]]), block(['CLOSE'], [[1, 2]])]:
            with self.assertRaises(ValueError):
                table({'history': value}, 'history')


if __name__ == '__main__':
    unittest.main()
