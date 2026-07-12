# Security Policy

## Supported versions

SteadyState is pre-1.0. Security fixes apply to the latest commit on `main`; older development tags are not maintained.

## Reporting a vulnerability

Use GitHub's private vulnerability reporting feature for this repository. Do not include secrets in issues, pull requests, workflow logs, or demonstration artifacts.

Include the affected component and version, reproduction steps, potential impact, required preconditions, and any suggested mitigation. Valid reports will be reproduced privately, fixed on a restricted branch when appropriate, and disclosed after a patched version is available.

## Security boundaries

The local kind environment is a development and demonstration platform, not a production security boundary. Docker Desktop, the host operating system, GitHub, and configured container registries remain trusted dependencies.
