import json
import unittest
from ci_go_summary import summarize


def events(*rows):
    return [json.dumps(row) + '\n' for row in rows]


class CISummaryTests(unittest.TestCase):
    def test_test_failure_retains_reason_without_duplicate_package_annotation(self):
        rows = events({'Action':'output','Package':'pkg','Test':'TestReal','Output':'fixture.go:42: wrong balance\n'},
                      {'Action':'fail','Package':'pkg','Test':'TestReal'},
                      {'Action':'output','Package':'pkg','Output':'FAIL\n'},
                      {'Action':'fail','Package':'pkg'})
        self.assertEqual(summarize(rows), ['::error::pkg/TestReal%0Afixture.go:42: wrong balance%0A'])

    def test_build_failure_includes_plain_compiler_output(self):
        rows = ['fixture.go:3: undefined: missing\n'] + events({'Action':'build-fail','Package':'pkg'})
        self.assertIn('undefined: missing', summarize(rows)[0])

    def test_success_and_skips_create_no_error_annotations(self):
        self.assertEqual(summarize(events({'Action':'pass','Package':'pkg','Test':'TestGood'},
                                         {'Action':'skip','Package':'pkg','Test':'TestPrivate'})), [])

    def test_multiline_output_cannot_inject_another_workflow_command(self):
        rows = events({'Action':'output','Package':'pkg','Test':'TestBad','Output':'100%\n::warning::injected\r\n'},
                      {'Action':'fail','Package':'pkg','Test':'TestBad'})
        result = summarize(rows)[0]
        self.assertNotIn('\n', result)
        self.assertIn('100%25%0A::warning::injected%0D%0A', result)


if __name__ == '__main__':
    unittest.main()
