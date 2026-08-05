package platformctl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type writeFlags struct {
	Plan                bool
	Team                string
	Owners              []string
	Repositories        []string
	CPU                 string
	Memory              string
	Owner               string
	ImageRepository     string
	ImageTag            string
	Port                int32
	MinReplicas         int32
	MaxReplicas         int32
	DatabaseRef         string
	Instances           int32
	Storage             string
	Schedule            string
	Retention           string
	SourceServer        string
	TargetTime          string
	DeletionRequest     string
	ApprovalRevision    string
	Force               bool
	AcknowledgeDataLoss bool
}

func addWriteCommands(team, application, database *cobra.Command, options *Options) {
	if team != nil {
		team.AddCommand(newTypedWriteCommand(options, "team.create", "create NAME", "Create a Team through a reviewed Git proposal"))
		team.AddCommand(newTypedWriteCommand(options, "team.update", "update NAME", "Update a Team through a reviewed Git proposal"))
		team.AddCommand(newTypedWriteCommand(options, "team.delete", "delete NAME", "Approve protected Team retirement"))
		team.AddCommand(newTypedWriteCommand(options, "team.finalize", "finalize NAME", "Finalize an approved Team retirement"))
	}
	if application != nil {
		application.AddCommand(newTypedWriteCommand(options, "app.create", "create NAME", "Create an Application through a reviewed Git proposal"))
		application.AddCommand(newTypedWriteCommand(options, "app.update", "update NAME", "Update an Application through a reviewed Git proposal"))
		application.AddCommand(newTypedWriteCommand(options, "app.delete", "delete NAME", "Approve protected Application retirement"))
		application.AddCommand(newTypedWriteCommand(options, "app.finalize", "finalize NAME", "Finalize an approved Application retirement"))
	}
	if database != nil {
		database.AddCommand(newTypedWriteCommand(options, "database.create", "create NAME", "Create a Database through a reviewed Git proposal"))
		database.AddCommand(newTypedWriteCommand(options, "database.update", "update NAME", "Update a Database through a reviewed Git proposal"))
		database.AddCommand(newTypedWriteCommand(options, "database.restore", "restore NAME", "Restore a new Database lifetime from an archive"))
		database.AddCommand(newTypedWriteCommand(options, "database.delete", "delete NAME", "Approve protected Database retirement"))
		database.AddCommand(newTypedWriteCommand(options, "database.finalize", "finalize NAME", "Finalize an approved Database retirement"))
	}
}

func newTypedWriteCommand(options *Options, operation, use, short string) *cobra.Command {
	flags := writeFlags{}
	command := &cobra.Command{
		Use: use, Short: short, Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, selected, err := options.loadContext()
			if err != nil {
				return err
			}
			ctx, cancel := options.commandContext(cmd.Context())
			defer cancel()
			baseSHA, err := repositoryBaseSHA(ctx, selected)
			if err != nil {
				return err
			}
			parameters := flags.parameters(args[0])
			request := NewChangeRequest(operation, baseSHA, parameters)
			set, err := RenderChange(selected.CheckoutPath, request)
			if err != nil {
				return err
			}
			diff, err := ChangeSetDiff(set)
			if err != nil {
				return err
			}
			if !options.Quiet && options.Format == "table" {
				_, _ = fmt.Fprint(options.Stdout, diff)
			}
			if flags.Plan {
				return printChangeSummary(options, set, "planned", "", diff)
			}
			if strings.HasSuffix(operation, ".finalize") {
				if err := verifyFinalization(ctx, selected, operation, parameters, baseSHA); err != nil {
					return err
				}
			}
			confirmed, err := confirmSubmission(options, operation, args[0])
			if err != nil {
				return err
			}
			if !confirmed {
				return exitError(ExitUsage, "proposal was not submitted")
			}
			encoded, err := request.Encode()
			if err != nil {
				return err
			}
			runURL, err := dispatchChange(ctx, selected, request, encoded)
			if err != nil {
				return err
			}
			return printChangeSummary(options, set, "submitted", runURL, diff)
		},
	}
	command.Flags().BoolVar(&flags.Plan, "plan", false, "render and show the deterministic change without submitting")
	if !strings.HasPrefix(operation, "team.") {
		command.Flags().StringVar(&flags.Team, "team", "", "owning Team name")
	}
	switch operation {
	case "team.create", "team.update":
		command.Flags().StringSliceVar(&flags.Owners, "owner", nil, "Team owner identity (repeatable)")
		command.Flags().StringSliceVar(&flags.Repositories, "allow-repository", nil, "allowed image repository pattern (repeatable)")
		command.Flags().StringVar(&flags.CPU, "cpu", "2", "Team CPU quota")
		command.Flags().StringVar(&flags.Memory, "memory", "4Gi", "Team memory quota")
	case "app.create", "app.update":
		command.Flags().StringVar(&flags.Owner, "owner", "platform-team", "Application owner")
		command.Flags().StringVar(&flags.ImageRepository, "image-repository", "", "OCI image repository")
		command.Flags().StringVar(&flags.ImageTag, "image-tag", "", "immutable image tag")
		command.Flags().Int32Var(&flags.Port, "port", 8080, "application port")
		command.Flags().Int32Var(&flags.MinReplicas, "min-replicas", 1, "minimum replicas")
		command.Flags().Int32Var(&flags.MaxReplicas, "max-replicas", 3, "maximum replicas")
		command.Flags().StringVar(&flags.DatabaseRef, "database", "", "same-Team Database name")
	case "database.create", "database.update", "database.restore":
		command.Flags().Int32Var(&flags.Instances, "instances", 1, "PostgreSQL instances")
		command.Flags().StringVar(&flags.Storage, "storage", "1Gi", "persistent storage size")
		command.Flags().StringVar(&flags.Schedule, "backup-schedule", "0 0 2 * * *", "six-field backup cron")
		command.Flags().StringVar(&flags.Retention, "backup-retention", "7d", "backup retention")
		if operation == "database.restore" {
			command.Flags().StringVar(&flags.SourceServer, "source-server-name", "", "prior backup server name")
			command.Flags().StringVar(&flags.TargetTime, "target-time", "", "optional RFC3339 UTC recovery target")
		}
	}
	if strings.HasSuffix(operation, ".delete") {
		command.Flags().BoolVar(&flags.Force, "force", false, "request emergency force deletion")
		command.Flags().BoolVar(&flags.AcknowledgeDataLoss, "acknowledge-data-loss", false, "acknowledge force-deletion data-loss risk")
	}
	if operation == "service.retire" {
		command.Flags().BoolVar(&flags.Force, "force", false, "request emergency force deletion")
		command.Flags().BoolVar(&flags.AcknowledgeDataLoss, "acknowledge-data-loss", false, "acknowledge force-deletion data-loss risk")
	}
	if strings.HasSuffix(operation, ".finalize") {
		command.Flags().StringVar(&flags.DeletionRequest, "deletion-request", "", "approval PR deletion-request UUID")
		command.Flags().StringVar(&flags.ApprovalRevision, "approval-revision", "", "merged approval commit")
	}
	return command
}

func verifyFinalization(ctx context.Context, selected Context, operation string, parameters ChangeParameters, baseSHA string) error {
	if _, err := runExternal(ctx, selected.CheckoutPath, "git", "merge-base", "--is-ancestor", parameters.ApprovalRevision, baseSHA); err != nil {
		return exitError(ExitConflict, "approval revision is not merged into current main")
	}
	client, err := NewClusterClient(selected)
	if err != nil {
		return err
	}
	argoName := parameters.Team
	if strings.HasPrefix(operation, "team.") {
		argoName = parameters.Name
	}
	argoApplication, err := client.Get(ctx, argoAppGVR, "argocd", argoName)
	if err != nil {
		return err
	}
	revision, _, _ := unstructured.NestedString(argoApplication.Object, "status", "sync", "revision")
	revisions, _, _ := unstructured.NestedStringSlice(argoApplication.Object, "status", "sync", "revisions")
	visible := revision == parameters.ApprovalRevision
	if len(revisions) > 0 {
		visible = true
		for _, item := range revisions {
			visible = visible && item == parameters.ApprovalRevision
		}
	}
	if !visible {
		return exitError(ExitConflict, "approval revision is not yet visible in the tenant Argo Application")
	}

	var live *unstructured.Unstructured
	switch {
	case strings.HasPrefix(operation, "team."):
		live, err = client.Get(ctx, teamGVR, "", parameters.Name)
	case strings.HasPrefix(operation, "app."):
		live, err = client.Get(ctx, applicationGVR, "team-"+parameters.Team, parameters.Name)
	case strings.HasPrefix(operation, "service."):
		catalog, catalogErr := LoadCatalog(selected.CheckoutPath)
		if catalogErr != nil {
			return catalogErr
		}
		tenant, service, catalogErr := findCatalogService(catalog, parameters.Name)
		if catalogErr != nil {
			return catalogErr
		}
		if service.OwnsTeam {
			live, err = client.Get(ctx, teamGVR, "", tenant.Name)
		} else if len(service.Components) > 0 {
			if _, entryErr := applicationEntry(tenant, service.Components[0]); entryErr == nil {
				live, err = client.Get(ctx, applicationGVR, "team-"+tenant.Name, service.Components[0])
			}
		}
		if live == nil && err == nil && service.OwnsDatabase && service.DatabaseRef != "" {
			live, err = client.Get(ctx, databaseGVR, "team-"+tenant.Name, service.DatabaseRef)
		}
	default:
		live, err = client.Get(ctx, databaseGVR, "team-"+parameters.Team, parameters.Name)
	}
	if err != nil {
		return err
	}
	if live == nil {
		return nil
	}
	if live.GetAnnotations()["steadystate.dev/deletion-request"] != parameters.DeletionRequest {
		return exitError(ExitConflict, "live resource does not carry the approved deletion request")
	}
	return nil
}

func (f writeFlags) parameters(name string) ChangeParameters {
	return ChangeParameters{
		Team: f.Team, Name: name, Owners: f.Owners, AllowedRepositories: f.Repositories, CPUQuota: f.CPU, MemoryQuota: f.Memory,
		Owner: f.Owner, ImageRepository: f.ImageRepository, ImageTag: f.ImageTag, Port: f.Port, MinReplicas: f.MinReplicas, MaxReplicas: f.MaxReplicas, DatabaseRef: f.DatabaseRef,
		Instances: f.Instances, StorageSize: f.Storage, BackupSchedule: f.Schedule, BackupRetention: f.Retention,
		SourceServerName: f.SourceServer, TargetTime: f.TargetTime, DeletionRequest: f.DeletionRequest, ApprovalRevision: f.ApprovalRevision,
		Force: f.Force, AcknowledgeDataLoss: f.AcknowledgeDataLoss,
	}
}

func repositoryBaseSHA(ctx context.Context, selected Context) (string, error) {
	status, err := runExternal(ctx, selected.CheckoutPath, "git", "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return "", err
	}
	if status != "" {
		return "", exitError(ExitConflict, "tracked checkout changes must be committed before proposing a write")
	}
	refspec := "refs/heads/" + selected.DefaultBranch + ":refs/remotes/origin/" + selected.DefaultBranch
	if _, err := runExternal(ctx, selected.CheckoutPath, "git", "fetch", "--quiet", "origin", refspec); err != nil {
		return "", exitError(ExitRemote, "refresh default branch: %v", err)
	}
	sha, err := runExternal(ctx, selected.CheckoutPath, "git", "rev-parse", "origin/"+selected.DefaultBranch)
	if err != nil {
		return "", err
	}
	if !gitObjectPattern.MatchString(sha) {
		return "", exitError(ExitRemote, "Git returned an invalid default-branch SHA")
	}
	head, err := runExternal(ctx, selected.CheckoutPath, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	if head != sha {
		return "", exitError(ExitConflict, "checkout HEAD does not match origin/%s; update the checkout before planning", selected.DefaultBranch)
	}
	return sha, nil
}

func confirmSubmission(options *Options, operation, name string) (bool, error) {
	if options.Quiet {
		return false, exitError(ExitUsage, "interactive Git writes cannot use --quiet")
	}
	_, _ = fmt.Fprintf(options.Stderr, "Submit %s for %s through GitHub Actions? [y/N]: ", operation, name)
	line, err := bufio.NewReader(io.LimitReader(options.Stdin, 16)).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	value := strings.ToLower(strings.TrimSpace(line))
	return value == "y" || value == "yes", nil
}

func dispatchChange(ctx context.Context, selected Context, request ChangeRequest, encoded string) (string, error) {
	arguments := []string{"workflow", "run", "platform-change.yml", "--repo", selected.Repository, "--ref", selected.DefaultBranch,
		"-f", "schema_version=" + request.SchemaVersion, "-f", "request_id=" + request.RequestID, "-f", "base_sha=" + request.BaseSHA, "-f", "proposal=" + encoded}
	if _, err := runExternal(ctx, selected.CheckoutPath, "gh", arguments...); err != nil {
		return "", exitError(ExitRemote, "dispatch platform change: %v", err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		run, err := findRequestRun(ctx, selected, request.RequestID)
		if err == nil && run.URL != "" {
			return run.URL, nil
		}
		select {
		case <-ctx.Done():
			return "", exitError(ExitTimeout, "timed out discovering the broker run")
		case <-time.After(2 * time.Second):
		}
	}
	return "", exitError(ExitTimeout, "broker was dispatched but its run was not discoverable within 60 seconds")
}

type requestRun struct {
	DatabaseID                            int64 `json:"databaseId"`
	DisplayTitle, Status, Conclusion, URL string
}

func findRequestRun(ctx context.Context, selected Context, requestID string) (requestRun, error) {
	raw, err := runExternal(ctx, selected.CheckoutPath, "gh", "run", "list", "--repo", selected.Repository, "--workflow", "platform-change.yml", "--event", "workflow_dispatch", "--limit", "30", "--json", "databaseId,displayTitle,status,conclusion,url")
	if err != nil {
		return requestRun{}, err
	}
	var runs []requestRun
	if err := json.Unmarshal([]byte(raw), &runs); err != nil {
		return requestRun{}, err
	}
	for _, run := range runs {
		if strings.Contains(run.DisplayTitle, requestID) {
			return run, nil
		}
	}
	return requestRun{}, exitError(ExitNotFound, "request %s has no workflow run", requestID)
}

func printChangeSummary(options *Options, set ChangeSet, state, runURL, diff string) error {
	paths := make([]string, 0, len(set.Files))
	for _, file := range set.Files {
		paths = append(paths, file.Action+":"+file.Path)
	}
	value := map[string]any{"state": state, "requestID": set.RequestID, "proposalDigest": set.ProposalDigest, "renderDigest": set.RenderDigest, "files": paths, "diff": diff, "runURL": runURL}
	return options.printer().Table([]string{"STATE", "REQUEST ID", "PROPOSAL DIGEST", "FILES", "RUN"}, [][]string{{state, set.RequestID, set.ProposalDigest, strconv.Itoa(len(set.Files)), runURL}}, value)
}

func newRequestCommand(options *Options) *cobra.Command {
	command := &cobra.Command{Use: "request", Short: "Inspect submitted platform change requests"}
	for _, action := range []string{"status", "open", "watch"} {
		action := action
		command.AddCommand(&cobra.Command{Use: action + " REQUEST_ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := uuid.Parse(args[0]); err != nil {
				return exitError(ExitUsage, "request ID must be a UUID")
			}
			_, selected, err := options.loadContext()
			if err != nil {
				return err
			}
			ctx, cancel := options.commandContext(cmd.Context())
			defer cancel()
			run, err := findRequestRun(ctx, selected, args[0])
			if err != nil {
				return err
			}
			if action == "watch" {
				_, err = runExternal(ctx, selected.CheckoutPath, "gh", "run", "watch", fmt.Sprint(run.DatabaseID), "--repo", selected.Repository, "--exit-status")
				return err
			}
			if action == "open" {
				_, err = runExternal(ctx, selected.CheckoutPath, "gh", "run", "view", fmt.Sprint(run.DatabaseID), "--repo", selected.Repository, "--web")
				return err
			}
			return options.printer().Table([]string{"REQUEST ID", "STATUS", "CONCLUSION", "URL"}, [][]string{{args[0], run.Status, run.Conclusion, run.URL}}, run)
		}})
	}
	return command
}

func newBrokerCommand(options *Options) *cobra.Command {
	command := &cobra.Command{Use: "broker", Hidden: true}
	for _, action := range []string{"validate", "apply"} {
		action := action
		var proposal string
		subcommand := &cobra.Command{Use: action, Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
			root, err := os.Getwd()
			if err != nil {
				return err
			}
			request, err := DecodeChangeRequest(proposal)
			if err != nil {
				return err
			}
			set, err := RenderChange(root, request)
			if err != nil {
				return err
			}
			if action == "apply" {
				if err := ApplyChangeSet(root, set); err != nil {
					return err
				}
			}
			data, err := ChangeSetJSON(set)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(options.Stdout, string(data))
			return err
		}}
		subcommand.Flags().StringVar(&proposal, "proposal", "", "strict Base64 proposal")
		_ = subcommand.MarkFlagRequired("proposal")
		command.AddCommand(subcommand)
	}
	return command
}
