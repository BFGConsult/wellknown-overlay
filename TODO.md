# TODO

Current backlog from the deployment/autoconfig work:

- Improve the gateway's overlay-only `index.html` into a small explanatory
  page that links to relevant standards documents and, once public, the
  wellknown-overlay project.
- Avoid exposing gateway `/healthz` publicly by default. As an overlay, the
  gateway should be careful about claiming unnecessary URLs because they can
  confuse users or mask routes that should belong to the underlying backend.
