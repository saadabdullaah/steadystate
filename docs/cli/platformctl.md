# platformctl command reference

Generated from the `v0.8` command tree. Do not edit manually.

## `platformctl`

Operate the SteadyState developer platform

```text
platformctl [flags]

--context string     configuration context
      --no-color           disable color output
  -o, --output string      output format: table, json, or yaml (default "table")
  -q, --quiet              suppress successful output
      --timeout duration   command timeout (default 30s)
```

## `platformctl app`

Read SteadyState Applications

```text
platformctl app [flags]
```

## `platformctl app abort`

Abort a canary Rollout using confirmed break glass

```text
platformctl app abort NAME [flags]

--confirm string     exact Application name confirmation
  -n, --namespace string   Team namespace
      --reason string      human reason recorded in break-glass audit evidence
```

## `platformctl app create`

Create an Application through a reviewed Git proposal

```text
platformctl app create NAME [flags]

--database string           same-Team Database name
      --image-repository string   OCI image repository
      --image-tag string          immutable image tag
      --max-replicas int32        maximum replicas (default 3)
      --min-replicas int32        minimum replicas (default 1)
      --owner string              Application owner (default "platform-team")
      --plan                      render and show the deterministic change without submitting
      --port int32                application port (default 8080)
      --team string               owning Team name
```

## `platformctl app delete`

Approve protected Application retirement

```text
platformctl app delete NAME [flags]

--acknowledge-data-loss   acknowledge force-deletion data-loss risk
      --force                   request emergency force deletion
      --plan                    render and show the deterministic change without submitting
      --team string             owning Team name
```

## `platformctl app doctor`

Diagnose Application health contracts

```text
platformctl app doctor NAME [flags]

-n, --namespace string   Team namespace
```

## `platformctl app finalize`

Finalize an approved Application retirement

```text
platformctl app finalize NAME [flags]

--approval-revision string   merged approval commit
      --deletion-request string    approval PR deletion-request UUID
      --plan                       render and show the deterministic change without submitting
      --team string                owning Team name
```

## `platformctl app list`

List Applications

```text
platformctl app list [flags]

-n, --namespace string   Team namespace (all namespaces when omitted)
```

## `platformctl app logs`

Read current or historical application logs

```text
platformctl app logs NAME [flags]

-f, --follow             follow the current Pod log stream
      --historical         query retained logs from Loki
  -n, --namespace string   Team namespace
      --since duration     lookback duration (default 1h0m0s)
      --tail int           maximum log lines (default 200)
```

## `platformctl app policy`

Show Kyverno policy results

```text
platformctl app policy NAME [flags]

-n, --namespace string   Team namespace
```

## `platformctl app promote`

Promote a canary Rollout using confirmed break glass

```text
platformctl app promote NAME [flags]

--confirm string     exact Application name confirmation
  -n, --namespace string   Team namespace
      --reason string      human reason recorded in break-glass audit evidence
```

## `platformctl app provenance`

Show active image and Git provenance

```text
platformctl app provenance NAME [flags]

-n, --namespace string   Team namespace
```

## `platformctl app rollout`

Show Rollout and AnalysisRun state

```text
platformctl app rollout NAME [flags]

-n, --namespace string   Team namespace
```

## `platformctl app slo`

Query application SLO and alert series

```text
platformctl app slo NAME [flags]

-n, --namespace string   Team namespace
```

## `platformctl app status`

Show Application status

```text
platformctl app status NAME [flags]

-n, --namespace string   Team namespace (derived from the catalog when omitted)
```

## `platformctl app traces`

Query application traces from Tempo

```text
platformctl app traces NAME [flags]

--limit int          maximum trace results (default 20)
  -n, --namespace string   Team namespace
      --trace-id string    fetch one exact trace ID
```

`--trace-id` accepts the canonical 32-character lowercase hexadecimal ID used
in application logs. Tempo's raw OTLP/protobuf JSON response represents its
`traceId` bytes field as Base64; these are two encodings of the same 16 bytes.

## `platformctl app update`

Update an Application through a reviewed Git proposal

```text
platformctl app update NAME [flags]

--database string           same-Team Database name
      --image-repository string   OCI image repository
      --image-tag string          immutable image tag
      --max-replicas int32        maximum replicas (default 3)
      --min-replicas int32        minimum replicas (default 1)
      --owner string              Application owner (default "platform-team")
      --plan                      render and show the deterministic change without submitting
      --port int32                application port (default 8080)
      --team string               owning Team name
```

## `platformctl cluster`

Manage the configured local cluster

```text
platformctl cluster [flags]
```

## `platformctl cluster down`

Delete the exact configured local cluster

```text
platformctl cluster down [flags]
```

## `platformctl cluster status`

Show the configured cluster status

```text
platformctl cluster status [flags]
```

## `platformctl cluster up`

Reconcile the configured local cluster

```text
platformctl cluster up [flags]
```

## `platformctl completion`

Generate shell completion

```text
platformctl completion [bash|zsh|fish|powershell] [flags]
```

## `platformctl config`

Manage platformctl configuration

```text
platformctl config [flags]
```

## `platformctl config init`

Create the initial non-secret configuration

```text
platformctl config init [flags]

--branch string         default Git branch (default "main")
      --checkout string       SteadyState checkout path
      --cluster string        kind cluster name (default "steadystate")
      --force                 replace an existing configuration after creating a backup
      --http-port int         Gateway HTTP host port (default 8080)
      --https-port int        Gateway HTTPS host port (default 8443)
      --kube-context string   Kubernetes context
      --name string           initial context name (default "local")
      --profile string        minimal, standard, or full (default "standard")
      --repository string     GitHub repository as OWNER/REPO (inferred from origin when omitted)
```

## `platformctl config view`

Show the redacted configuration

```text
platformctl config view [flags]
```

## `platformctl context`

Manage named platform contexts

```text
platformctl context [flags]
```

## `platformctl context delete`

Delete a non-current context

```text
platformctl context delete NAME [flags]
```

## `platformctl context list`

List contexts

```text
platformctl context list [flags]
```

## `platformctl context set`

Create or replace a context

```text
platformctl context set NAME [flags]

--branch string         default Git branch (default "main")
      --checkout string       SteadyState checkout path
      --cluster string        kind cluster name (default "steadystate")
      --http-port int         Gateway HTTP host port (default 8080)
      --https-port int        Gateway HTTPS host port (default 8443)
      --kube-context string   Kubernetes context
      --kubeconfig string     kubeconfig path
      --profile string        minimal, standard, or full (default "standard")
      --repository string     GitHub repository as OWNER/REPO (default "saadabdullaah/steadystate")
```

## `platformctl context use`

Select the current context

```text
platformctl context use NAME [flags]
```

## `platformctl database`

Read SteadyState Databases

```text
platformctl database [flags]
```

## `platformctl database backups`

List CloudNativePG backups

```text
platformctl database backups NAME [flags]

-n, --namespace string   Team namespace
```

## `platformctl database create`

Create a Database through a reviewed Git proposal

```text
platformctl database create NAME [flags]

--backup-retention string   backup retention (default "7d")
      --backup-schedule string    six-field backup cron (default "0 0 2 * * *")
      --instances int32           PostgreSQL instances (default 1)
      --plan                      render and show the deterministic change without submitting
      --storage string            persistent storage size (default "1Gi")
      --team string               owning Team name
```

## `platformctl database delete`

Approve protected Database retirement

```text
platformctl database delete NAME [flags]

--acknowledge-data-loss   acknowledge force-deletion data-loss risk
      --force                   request emergency force deletion
      --plan                    render and show the deterministic change without submitting
      --team string             owning Team name
```

## `platformctl database finalize`

Finalize an approved Database retirement

```text
platformctl database finalize NAME [flags]

--approval-revision string   merged approval commit
      --deletion-request string    approval PR deletion-request UUID
      --plan                       render and show the deterministic change without submitting
      --team string                owning Team name
```

## `platformctl database restore`

Restore a new Database lifetime from an archive

```text
platformctl database restore NAME [flags]

--backup-retention string     backup retention (default "7d")
      --backup-schedule string      six-field backup cron (default "0 0 2 * * *")
      --instances int32             PostgreSQL instances (default 1)
      --plan                        render and show the deterministic change without submitting
      --source-server-name string   prior backup server name
      --storage string              persistent storage size (default "1Gi")
      --target-time string          optional RFC3339 UTC recovery target
      --team string                 owning Team name
```

## `platformctl database status`

Show Database status

```text
platformctl database status NAME [flags]

-n, --namespace string   Team namespace (derived from the catalog when omitted)
```

## `platformctl database update`

Update a Database through a reviewed Git proposal

```text
platformctl database update NAME [flags]

--backup-retention string   backup retention (default "7d")
      --backup-schedule string    six-field backup cron (default "0 0 2 * * *")
      --instances int32           PostgreSQL instances (default 1)
      --plan                      render and show the deterministic change without submitting
      --storage string            persistent storage size (default "1Gi")
      --team string               owning Team name
```

## `platformctl dev`

Run a generated service with a host-native edit loop

```text
platformctl dev NAME [flags]

--bootstrap         bootstrap the configured local profile before starting
      --database-tunnel   forward and inject the configured PostgreSQL connection
```

## `platformctl doctor`

Validate local and remote platform prerequisites

```text
platformctl doctor [flags]
```

## `platformctl help`

Help about any command

```text
platformctl help [command] [flags]
```

## `platformctl init`

Scaffold a golden-path service through a reviewed Git proposal

```text
platformctl init NAME [flags]

--create-team       create an isolated Team owned by this service
      --plan              render and show the deterministic change without submitting
      --team string       existing Team name (defaults to NAME)
      --template string   golden template: go-api, react-static, or full-stack
      --with-database     create and attach PostgreSQL (full-stack only)
```

## `platformctl platform`

Manage the complete local SteadyState platform

```text
platformctl platform [flags]
```

## `platformctl platform down`

Stop the exact configured platform while retaining backups

```text
platformctl platform down [flags]
```

## `platformctl platform status`

Show aggregate platform status

```text
platformctl platform status [flags]
```

## `platformctl platform up`

Reconcile the complete configured platform

```text
platformctl platform up [flags]
```

## `platformctl platform verify`

Verify the configured complete platform

```text
platformctl platform verify [flags]
```

## `platformctl portal`

Run the loopback-only SteadyState developer portal

```text
platformctl portal [flags]

--no-open    do not open the system browser
      --port int   fixed loopback port (zero selects an available port)
```

## `platformctl profile`

Inspect supported local cluster profiles

```text
platformctl profile [flags]
```

## `platformctl profile list`

List supported local cluster profiles

```text
platformctl profile list [flags]
```

## `platformctl request`

Inspect submitted platform change requests

```text
platformctl request [flags]
```

## `platformctl request open`

Operate this platformctl command.

```text
platformctl request open REQUEST_ID [flags]
```

## `platformctl request status`

Operate this platformctl command.

```text
platformctl request status REQUEST_ID [flags]
```

## `platformctl request watch`

Operate this platformctl command.

```text
platformctl request watch REQUEST_ID [flags]
```

## `platformctl service`

Manage generated-service lifecycle

```text
platformctl service [flags]
```

## `platformctl service finalize`

Finalize an approved service retirement

```text
platformctl service finalize NAME [flags]

--approval-revision string   merged approval commit
      --deletion-request string    approval PR deletion-request UUID
      --plan                       render and show the deterministic change without submitting
      --team string                owning Team name
```

## `platformctl service retire`

Approve protected service retirement

```text
platformctl service retire NAME [flags]

--acknowledge-data-loss   acknowledge force-deletion data-loss risk
      --force                   request emergency force deletion
      --plan                    render and show the deterministic change without submitting
      --team string             owning Team name
```

## `platformctl team`

Read SteadyState Teams

```text
platformctl team [flags]
```

## `platformctl team create`

Create a Team through a reviewed Git proposal

```text
platformctl team create NAME [flags]

--allow-repository strings   allowed image repository pattern (repeatable)
      --cpu string                 Team CPU quota (default "2")
      --memory string              Team memory quota (default "4Gi")
      --owner strings              Team owner identity (repeatable)
      --plan                       render and show the deterministic change without submitting
```

## `platformctl team delete`

Approve protected Team retirement

```text
platformctl team delete NAME [flags]

--acknowledge-data-loss   acknowledge force-deletion data-loss risk
      --force                   request emergency force deletion
      --plan                    render and show the deterministic change without submitting
```

## `platformctl team finalize`

Finalize an approved Team retirement

```text
platformctl team finalize NAME [flags]

--approval-revision string   merged approval commit
      --deletion-request string    approval PR deletion-request UUID
      --plan                       render and show the deterministic change without submitting
```

## `platformctl team list`

List Teams from the Git catalog

```text
platformctl team list [flags]
```

## `platformctl team status`

Show Team status

```text
platformctl team status NAME [flags]
```

## `platformctl team update`

Update a Team through a reviewed Git proposal

```text
platformctl team update NAME [flags]

--allow-repository strings   allowed image repository pattern (repeatable)
      --cpu string                 Team CPU quota (default "2")
      --memory string              Team memory quota (default "4Gi")
      --owner strings              Team owner identity (repeatable)
      --plan                       render and show the deterministic change without submitting
```

## `platformctl version`

Print platformctl build information

```text
platformctl version [flags]
```
