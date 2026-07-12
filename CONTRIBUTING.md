# Contributing to SteadyState

## Workflow

1. Start from an up-to-date `main` branch.
2. Create a short-lived branch named `phase<N>/<short-description>`.
3. Use Conventional Commits: `feat:`, `fix:`, `docs:`, `test:`, `refactor:`, or `chore:`.
4. Run relevant local checks before opening a pull request.
5. Merge only through a green pull request using squash merge.

```powershell
git switch main
git pull --ff-only
git switch -c phase0/example-change
.\scripts\dev.ps1 lint
.\scripts\dev.ps1 test
git push -u origin phase0/example-change
```

## Engineering rules

- Windows Git is authoritative for local repository operations.
- Use repository-local tools rather than global versions.
- Pin external versions and images; avoid mutable `latest` references.
- Keep reconciliation code deterministic and testable.
- Add acceptance tests for observable behavior.
- Record important decisions in `docs/adr/`.
- Never commit kubeconfigs, credentials, private keys, decrypted secrets, or private planning material.
- Public documentation must describe implemented and verified behavior.

Pull requests must explain the outcome, verification, and operational impact. Required checks must pass and review conversations must be resolved. The repository uses linear history and squash merges.

Do not open a public issue for a vulnerability or suspected credential exposure. Follow [SECURITY.md](SECURITY.md).
