# Security Policy

## Supported versions

Security fixes apply to the latest supported release and the latest commit on `main`; older development tags are not maintained.

## Reporting a vulnerability

Use GitHub's private vulnerability reporting feature for this repository. Do not include secrets in issues, pull requests, workflow logs, or demonstration artifacts.

Include the affected component and version, reproduction steps, potential impact, required preconditions, and any suggested mitigation. Valid reports will be reproduced privately, fixed on a restricted branch when appropriate, and disclosed after a patched version is available.

## Security boundaries

The local kind environment is a development and demonstration platform, not a production security boundary. Docker Desktop, the host operating system, GitHub, and configured container registries remain trusted dependencies.

The v1.0 portal is a local-owner interface, not a multi-user authorization
boundary. It listens only on `127.0.0.1` and uses a single-use launch token,
HttpOnly SameSite cookie, exact Host/Origin checks, CSRF token, strict content
policy, size/rate limits, and typed allowlisted operations. The local account,
browser, checkout, `gh` authentication, and cluster-admin kubeconfig remain
trusted. Do not proxy or expose the portal to a LAN or public network.
