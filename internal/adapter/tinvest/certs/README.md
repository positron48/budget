# T-Invest TLS trust

The client embeds only the RSA **Russian Trusted Root CA** in its own TLS
certificate pool. Hostname, validity and chain verification remain enabled;
redirects remain disabled. System Keychain and other HTTP clients are unchanged.

Source, verified 2026-10-08:

- T-Bank instructions: https://status.tbank-online.com/certificates/ (macOS link).
- Official download: https://gu-st.ru/content/lending/russiantrustedca.zip
- Root extracted from `russiantrustedca2024.pem` in that archive; intermediate
  certificates are not installed as trust anchors.
- SHA-256 fingerprint (DER):
  `D26D2D0231B7C39F92CC738512BA54103519E4405D68B5BD703E9788CA8ECF31`.
- Valid through 2032-02-27 21:04:15 UTC.

Download and renewal must use verified HTTPS from the official source. Review
the issuer, validity and fingerprint before replacing the bundled certificate.
Do not enable `InsecureSkipVerify` as a fallback. The PEM contains only a public
CA certificate; the API token remains in Keychain.
