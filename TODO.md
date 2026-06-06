# TODO

Current backlog from the deployment/autoconfig work:

- Improve the gateway's overlay-only `index.html` into a small explanatory
  page that links to relevant standards documents and, once public, the
  wellknown-overlay project.
- Avoid exposing gateway `/healthz` publicly by default. As an overlay, the
  gateway should be careful about claiming unnecessary URLs because they can
  confuse users or mask routes that should belong to the underlying backend.
- Improve the mail setup PO structure. The current Norwegian translation uses
  one large page-level message, which is poor translator ergonomics and awkward
  for Weblate. Break the setup page into smaller stable translation units while
  keeping the rendered page server-side and placeholder-safe.
- Add a translation extraction/update build step. When English source strings
  change, the PO template and language files should be updated so translation
  status degrades per changed string, not to zero for the whole page unless the
  page is genuinely rewritten.
- Explore an online version of the live tester where a user can enter an email
  address, optionally add a password, and verify the discovered IMAP/SMTP setup
  end-to-end. Password checks are useful with throwaway accounts during early
  testing, but publication still needs an explicit security model: avoid storing
  credentials, keep logs scrubbed, rate-limit attempts, require HTTPS, and make
  the test boundaries clear to users.
