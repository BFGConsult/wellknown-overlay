# TODO

Current backlog from the deployment/autoconfig work:

- Support host-aware gateway behavior so one overlay deployment can serve
  different domains differently, for example `autoconfig.example.org` as an
  overlay-only host with a small fallback page while `example.org` keeps
  proxying non-overlay routes to the backend.
- Improve the gateway's overlay-only `index.html` into a small explanatory
  page that links to relevant standards documents and, once public, the
  wellknown-overlay project.
