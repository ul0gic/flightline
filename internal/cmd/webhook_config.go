package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

const webhookSecretMaxBytes = 64 << 10

var webhookSecretRefPattern = regexp.MustCompile(`^env:[A-Z_][A-Z0-9_]*$`)

type WebhookListResult struct {
	Webhooks []asc.WebhookConfig `json:"webhooks"`
}

func (r WebhookListResult) TableRows() (headers []string, rows [][]string) {
	rows = make([][]string, 0, len(r.Webhooks))
	for _, hook := range r.Webhooks {
		rows = append(rows, []string{hook.ID, hook.Name, hook.Endpoint, boolPtrStr(hook.Enabled), strings.Join(hook.EventTypes, ",")})
	}
	return []string{"ID", "NAME", "ENDPOINT", "ENABLED", "EVENT_TYPES"}, rows
}

type WebhookActionResult struct {
	Action        string            `json:"action"`
	Webhook       asc.WebhookConfig `json:"webhook"`
	NoOp          bool              `json:"noOp"`
	SecretRotated bool              `json:"secretRotated"`
}

func (r WebhookActionResult) TableRows() (headers []string, rows [][]string) {
	return []string{"ACTION", "ID", "NAME", "ENDPOINT", "NOOP", "SECRET_ROTATED"}, [][]string{{r.Action, r.Webhook.ID, r.Webhook.Name, r.Webhook.Endpoint, strconv.FormatBool(r.NoOp), strconv.FormatBool(r.SecretRotated)}}
}

type WebhookDeleteResult struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}

func (r WebhookDeleteResult) TableRows() (headers []string, rows [][]string) {
	return []string{"ID", "DELETED"}, [][]string{{r.ID, strconv.FormatBool(r.Deleted)}}
}

func newWebhooksCommand() *cobra.Command {
	group := &cobra.Command{Use: "webhooks", Short: "Manage app webhook configuration and inspect deliveries"}
	group.AddCommand(&cobra.Command{Use: "list <bundleId>", Short: "List app webhook configurations", Args: cobra.ExactArgs(1), SilenceUsage: true, RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		return runWebhooksList(cmd, c, args[0], outputMode())
	}})
	group.AddCommand(&cobra.Command{Use: "get <bundleId> <webhookId>", Short: "Inspect an app-owned webhook", Args: cobra.ExactArgs(2), SilenceUsage: true, RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		return runWebhooksGet(cmd, c, args[0], args[1], outputMode())
	}})
	var create webhookFlags
	createCmd := &cobra.Command{Use: "create <bundleId>", Short: "Create a confirmed webhook configuration", Args: cobra.ExactArgs(1), SilenceUsage: true, RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		return runWebhooksCreate(cmd, c, args[0], create, outputMode())
	}}
	bindWebhookFlags(createCmd, &create)
	group.AddCommand(createCmd)
	var update webhookFlags
	updateCmd := &cobra.Command{Use: "update <bundleId> <webhookId>", Short: "Update or rotate a confirmed webhook configuration", Args: cobra.ExactArgs(2), SilenceUsage: true, RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		return runWebhooksUpdate(cmd, c, args[0], args[1], update, outputMode())
	}}
	bindWebhookFlags(updateCmd, &update)
	group.AddCommand(updateCmd)
	var confirmDelete bool
	deleteCmd := &cobra.Command{Use: "delete <bundleId> <webhookId>", Short: "Delete a confirmed app webhook", Args: cobra.ExactArgs(2), SilenceUsage: true, RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		return runWebhooksDelete(cmd, c, args[0], args[1], confirmDelete, outputMode())
	}}
	deleteCmd.Flags().BoolVar(&confirmDelete, "confirm", false, "confirm webhook deletion")
	group.AddCommand(deleteCmd)
	group.AddCommand(newWebhookPingCommand(), newWebhookDeliveriesCommand())
	return group
}

type webhookFlags struct {
	name, endpoint, secretRef, secretFile string
	events                                []string
	enabled, confirm                      bool
}

func bindWebhookFlags(cmd *cobra.Command, flags *webhookFlags) {
	cmd.Flags().StringVar(&flags.name, "name", "", "webhook name")
	cmd.Flags().StringVar(&flags.endpoint, "url", "", "HTTPS webhook endpoint without URL credentials")
	cmd.Flags().StringArrayVar(&flags.events, "event", nil, "event type (repeatable)")
	cmd.Flags().BoolVar(&flags.enabled, "enabled", false, "enable the webhook")
	cmd.Flags().StringVar(&flags.secretRef, "secret-ref", "", "env:NAME signing-secret reference")
	cmd.Flags().StringVar(&flags.secretFile, "secret-file", "", "private regular file with signing secret")
	cmd.Flags().BoolVar(&flags.confirm, "confirm", false, "confirm webhook mutation")
}

func resolveWebhookApp(ctx context.Context, c *asc.Client, bundleID string) (string, error) {
	appID, err := resolveAppID(ctx, c, bundleID)
	if err != nil {
		return "", asc.SafeWebhookReadError("resolve app", err)
	}
	app, err := asc.Get[asc.Single[AppAttributes]](ctx, c, "/v1/apps/"+url.PathEscape(appID), nil)
	if err != nil {
		return "", asc.SafeWebhookReadError("verify app", err)
	}
	if app.Data.Type != "apps" || app.Data.ID != appID || !isNumericAppID(bundleID) && app.Data.Attributes.BundleID != bundleID {
		return "", fmt.Errorf("webhooks: app %q identity is inconsistent", bundleID)
	}
	return appID, nil
}

func webhookOwned(ctx context.Context, c *asc.Client, appID, webhookID string) (asc.WebhookConfig, error) {
	rows, err := asc.ListAppWebhooks(ctx, c, appID)
	if err != nil {
		return asc.WebhookConfig{}, err
	}
	for _, row := range rows {
		if row.ID == webhookID {
			return row, nil
		}
	}
	return asc.WebhookConfig{}, fmt.Errorf("webhook %q is not owned by the selected app", webhookID)
}

func runWebhooksList(cmd *cobra.Command, c *asc.Client, bundleID, output string) error {
	appID, err := resolveWebhookApp(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}
	rows, err := asc.ListAppWebhooks(cmd.Context(), c, appID)
	if err != nil {
		return err
	}
	return renderTo(cmd.OutOrStdout(), WebhookListResult{Webhooks: rows}, output, true)
}

func runWebhooksGet(cmd *cobra.Command, c *asc.Client, bundleID, webhookID, output string) error {
	appID, err := resolveWebhookApp(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}
	row, err := webhookOwned(cmd.Context(), c, appID, webhookID)
	if err != nil {
		return err
	}
	return renderTo(cmd.OutOrStdout(), WebhookActionResult{Action: "get", Webhook: row}, output, true)
}

func runWebhooksCreate(cmd *cobra.Command, c *asc.Client, bundleID string, flags webhookFlags, output string) error {
	if !flags.confirm {
		return errors.New("webhook create requires --confirm")
	}
	attrs, err := webhookCreateAttrs(cmd, flags)
	if err != nil {
		return err
	}
	appID, err := resolveWebhookApp(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}
	id, err := asc.CreateAppWebhook(cmd.Context(), c, appID, attrs)
	if err != nil {
		return err
	}
	row, err := webhookOwned(cmd.Context(), c, appID, id)
	if err != nil {
		return &asc.WebhookMutationError{Action: "create"}
	}
	return renderTo(cmd.OutOrStdout(), WebhookActionResult{Action: "create", Webhook: row}, output, true)
}

func webhookCreateAttrs(cmd *cobra.Command, flags webhookFlags) (asc.WebhookWriteAttributes, error) {
	if strings.TrimSpace(flags.name) == "" || !cmd.Flags().Changed("enabled") {
		return asc.WebhookWriteAttributes{}, errors.New("webhook create requires --name and explicit --enabled")
	}
	if err := asc.ValidateWebhookURL(flags.endpoint); err != nil {
		return asc.WebhookWriteAttributes{}, err
	}
	if err := asc.ValidateWebhookEvents(flags.events); err != nil {
		return asc.WebhookWriteAttributes{}, err
	}
	secret, err := readWebhookSecret(flags.secretRef, flags.secretFile)
	if err != nil {
		return asc.WebhookWriteAttributes{}, err
	}
	return asc.WebhookWriteAttributes{Name: &flags.name, URL: &flags.endpoint, EventTypes: &flags.events, Enabled: &flags.enabled, Secret: &secret}, nil
}

func runWebhooksUpdate(cmd *cobra.Command, c *asc.Client, bundleID, webhookID string, flags webhookFlags, output string) error {
	if !flags.confirm {
		return errors.New("webhook update requires --confirm")
	}
	appID, err := resolveWebhookApp(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}
	current, err := webhookOwned(cmd.Context(), c, appID, webhookID)
	if err != nil {
		return err
	}
	attrs, rotated, err := webhookUpdateAttrs(cmd, current, flags)
	if err != nil {
		return err
	}
	if attrs.Name == nil && attrs.Enabled == nil && attrs.EventTypes == nil && attrs.URL == nil && attrs.Secret == nil {
		return renderTo(cmd.OutOrStdout(), WebhookActionResult{Action: "update", Webhook: current, NoOp: true}, output, true)
	}
	if err := asc.PatchWebhook(cmd.Context(), c, webhookID, attrs); err != nil {
		return err
	}
	after, err := webhookOwned(cmd.Context(), c, appID, webhookID)
	if err != nil {
		return &asc.WebhookMutationError{Action: "update"}
	}
	if !webhookMatchesUpdate(after, attrs) {
		return &asc.WebhookMutationError{Action: "update"}
	}
	return renderTo(cmd.OutOrStdout(), WebhookActionResult{Action: "update", Webhook: after, SecretRotated: rotated}, output, true)
}

func webhookUpdateAttrs(cmd *cobra.Command, current asc.WebhookConfig, flags webhookFlags) (asc.WebhookWriteAttributes, bool, error) {
	attrs, err := webhookConfigFieldAttrs(cmd, current, flags)
	if err != nil {
		return attrs, false, err
	}
	rotated := cmd.Flags().Changed("secret-ref") || cmd.Flags().Changed("secret-file") || flags.secretRef != "" || flags.secretFile != ""
	if rotated {
		secret, err := readWebhookSecret(flags.secretRef, flags.secretFile)
		if err != nil {
			return attrs, false, err
		}
		attrs.Secret = &secret
	}
	return attrs, rotated, nil
}

func webhookConfigFieldAttrs(cmd *cobra.Command, current asc.WebhookConfig, flags webhookFlags) (asc.WebhookWriteAttributes, error) {
	var attrs asc.WebhookWriteAttributes
	if cmd.Flags().Changed("name") {
		if strings.TrimSpace(flags.name) == "" {
			return attrs, errors.New("webhook name must be nonempty")
		}
		if flags.name != current.Name {
			attrs.Name = &flags.name
		}
	}
	if cmd.Flags().Changed("url") {
		if err := asc.ValidateWebhookURL(flags.endpoint); err != nil {
			return attrs, err
		}
		if flags.endpoint != current.URL {
			attrs.URL = &flags.endpoint
		}
	}
	if cmd.Flags().Changed("event") {
		if err := asc.ValidateWebhookEvents(flags.events); err != nil {
			return attrs, err
		}
		if !sameWebhookEvents(flags.events, current.EventTypes) {
			attrs.EventTypes = &flags.events
		}
	}
	if cmd.Flags().Changed("enabled") && (current.Enabled == nil || flags.enabled != *current.Enabled) {
		attrs.Enabled = &flags.enabled
	}
	return attrs, nil
}

func webhookMatchesUpdate(current asc.WebhookConfig, attrs asc.WebhookWriteAttributes) bool {
	return (attrs.Name == nil || current.Name == *attrs.Name) &&
		(attrs.URL == nil || current.URL == *attrs.URL) &&
		(attrs.Enabled == nil || current.Enabled != nil && *current.Enabled == *attrs.Enabled) &&
		(attrs.EventTypes == nil || sameWebhookEvents(current.EventTypes, *attrs.EventTypes))
}

func sameWebhookEvents(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

func runWebhooksDelete(cmd *cobra.Command, c *asc.Client, bundleID, webhookID string, confirm bool, output string) error {
	if !confirm {
		return errors.New("webhook delete requires --confirm")
	}
	appID, err := resolveWebhookApp(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}
	if _, err := webhookOwned(cmd.Context(), c, appID, webhookID); err != nil {
		return err
	}
	writeErr := asc.DeleteWebhook(cmd.Context(), c, webhookID)
	rows, readErr := asc.ListAppWebhooks(cmd.Context(), c, appID)
	if readErr == nil && !containsWebhook(rows, webhookID) {
		return renderTo(cmd.OutOrStdout(), WebhookDeleteResult{ID: webhookID, Deleted: true}, output, true)
	}
	if writeErr != nil {
		return writeErr
	}
	return &asc.WebhookMutationError{Action: "delete"}
}

func containsWebhook(rows []asc.WebhookConfig, id string) bool {
	for _, row := range rows {
		if row.ID == id {
			return true
		}
	}
	return false
}

func readWebhookSecret(ref, path string) (string, error) {
	if (ref == "") == (path == "") {
		return "", errors.New("choose exactly one --secret-ref or --secret-file")
	}
	if ref != "" {
		if !webhookSecretRefPattern.MatchString(ref) {
			return "", errors.New("secret reference must be env:NAME")
		}
		secret, ok := os.LookupEnv(strings.TrimPrefix(ref, "env:"))
		if !ok {
			return "", errors.New("secret environment variable is unset")
		}
		return validateWebhookSecret(secret)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("secret file must be a private regular file")
	}
	file, err := os.Open(path) //nolint:gosec // The user explicitly selects a private signing-secret file.
	if err != nil {
		return "", errors.New("cannot open secret file")
	}
	defer func() { _ = file.Close() }()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return "", errors.New("secret file changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, webhookSecretMaxBytes+1))
	if err != nil {
		return "", errors.New("cannot read secret file")
	}
	if len(data) > webhookSecretMaxBytes {
		return "", errors.New("secret file exceeds size limit")
	}
	return validateWebhookSecret(strings.TrimRight(string(data), "\r\n"))
}

func validateWebhookSecret(secret string) (string, error) {
	if strings.TrimSpace(secret) == "" || len(secret) > webhookSecretMaxBytes {
		return "", errors.New("signing secret must be nonempty and within size limit")
	}
	return secret, nil
}
