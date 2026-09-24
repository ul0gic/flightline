package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

type AppEventsResult struct {
	Events []asc.AppEventView `json:"events"`
}

func (r AppEventsResult) TableRows() (headers []string, rows [][]string) {
	rows = make([][]string, 0, len(r.Events))
	for i := range r.Events {
		e := &r.Events[i]
		rows = append(rows, []string{e.ID, e.Attributes.ReferenceName, e.Attributes.EventState})
	}
	return []string{"ID", "REFERENCE", "STATE"}, rows
}

type AppEventActionResult struct {
	Action   string                      `json:"action"`
	ID       string                      `json:"id"`
	Changed  bool                        `json:"changed"`
	Event    *asc.AppEventView           `json:"event,omitempty"`
	Proposal *asc.SubmissionItemProposal `json:"proposal,omitempty"`
}

func (r AppEventActionResult) TableRows() (headers []string, rows [][]string) {
	return []string{"ACTION", "ID", "CHANGED"}, [][]string{{r.Action, r.ID, strconv.FormatBool(r.Changed)}}
}

func newEventsCommand() *cobra.Command {
	root := &cobra.Command{Use: "events", Short: "Manage In-App Events"}
	root.AddCommand(newEventsListCommand(), newEventsGetCommand(), newEventsCreateCommand(), newEventsUpdateCommand(), newEventsDeleteCommand(), newEventsProposeCommand(), newEventLocalizationsCommand(), newEventMediaCommand())
	return root
}

func eventTargetFlags(cmd *cobra.Command) { cmd.Flags().String("event", "", "In-App Event ID") }
func eventTarget(cmd *cobra.Command) (string, error) {
	id, _ := cmd.Flags().GetString("event")
	if id == "" {
		return "", errors.New("--event is required")
	}
	return id, nil
}
func eventClient(ctx context.Context, bundle string) (*asc.Client, string, error) {
	c, err := newClient()
	if err != nil {
		return nil, "", err
	}
	id, err := resolveAppID(ctx, c, bundle)
	if err != nil {
		return nil, "", err
	}
	return c, id, nil
}
func requireEventConfirm(cmd *cobra.Command) error {
	ok, _ := cmd.Flags().GetBool("confirm")
	if !ok {
		return errors.New("event mutation requires --confirm")
	}
	return nil
}

func newEventsListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list <bundleId>", Short: "List all In-App Events", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, appID, err := eventClient(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		events, err := asc.ListAppEvents(cmd.Context(), c, appID)
		if err != nil {
			return err
		}
		return Render(AppEventsResult{Events: events}, outputMode())
	}
	return cmd
}
func newEventsGetCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "get <bundleId>", Short: "Read one owned In-App Event", Args: cobra.ExactArgs(1)}
	eventTargetFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		id, err := eventTarget(cmd)
		if err != nil {
			return err
		}
		c, appID, err := eventClient(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		event, err := asc.FindAppEvent(cmd.Context(), c, appID, id)
		if err != nil {
			return err
		}
		return Render(AppEventActionResult{Action: "get", ID: id, Event: &event}, outputMode())
	}
	return cmd
}

func eventFields(cmd *cobra.Command) {
	cmd.Flags().String("reference-name", "", "internal event reference name")
	cmd.Flags().String("badge", "", "event badge enum")
	cmd.Flags().String("deep-link", "", "event absolute deep link")
	cmd.Flags().String("purchase-requirement", "", "purchase requirement text")
	cmd.Flags().String("primary-locale", "", "primary localization locale")
	cmd.Flags().String("priority", "", "HIGH or NORMAL")
	cmd.Flags().String("purpose", "", "event purpose enum")
	cmd.Flags().String("territory-schedules", "", "JSON array of territories and RFC3339 publishStart/eventStart/eventEnd")
}

func eventInput(cmd *cobra.Command, base asc.AppEventAttributes, create bool) (asc.AppEventAttributes, map[string]any, error) {
	fields := []struct{ flag, wire string }{{"reference-name", "referenceName"}, {"badge", "badge"}, {"deep-link", "deepLink"}, {"purchase-requirement", "purchaseRequirement"}, {"primary-locale", "primaryLocale"}, {"priority", "priority"}, {"purpose", "purpose"}}
	patch := make(map[string]any)
	for _, f := range fields {
		if cmd.Flags().Changed(f.flag) {
			v, _ := cmd.Flags().GetString(f.flag)
			patch[f.wire] = v
		}
	}
	if cmd.Flags().Changed("territory-schedules") {
		raw, _ := cmd.Flags().GetString("territory-schedules")
		var schedules []asc.AppEventTerritorySchedule
		if err := json.Unmarshal([]byte(raw), &schedules); err != nil {
			return base, nil, fmt.Errorf("territory schedules: %w", err)
		}
		patch["territorySchedules"] = schedules
	}
	if create && patch["referenceName"] == nil {
		return base, nil, errors.New("create requires --reference-name")
	}
	base, err := mergeEventInput(base, patch)
	if err != nil {
		return base, nil, err
	}
	if err := asc.ValidateAppEventAttributes(base, time.Now().UTC()); err != nil {
		return base, nil, err
	}
	return base, patch, nil
}

func mergeEventInput(base asc.AppEventAttributes, patch map[string]any) (asc.AppEventAttributes, error) {
	data, err := json.Marshal(base)
	if err != nil {
		return base, err
	}
	var values map[string]any
	if err := json.Unmarshal(data, &values); err != nil {
		return base, err
	}
	for key, value := range patch {
		values[key] = value
	}
	data, err = json.Marshal(values)
	if err != nil {
		return base, err
	}
	var result asc.AppEventAttributes
	if err := json.Unmarshal(data, &result); err != nil {
		return base, err
	}
	return result, nil
}

func newEventsCreateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "create <bundleId>", Short: "Create a draft In-App Event", Args: cobra.ExactArgs(1)}
	eventFields(cmd)
	cmd.Flags().Bool("confirm", false, "confirm event creation")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireEventConfirm(cmd); err != nil {
			return err
		}
		attrs, _, err := eventInput(cmd, asc.AppEventAttributes{}, true)
		if err != nil {
			return err
		}
		c, appID, err := eventClient(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		event, changed, err := asc.CreateAppEvent(cmd.Context(), c, appID, attrs, time.Now().UTC())
		if err != nil {
			return err
		}
		return Render(AppEventActionResult{Action: "create", ID: event.ID, Changed: changed, Event: &event}, outputMode())
	}
	return cmd
}
func newEventsUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "update <bundleId>", Short: "Update a draft In-App Event", Args: cobra.ExactArgs(1)}
	eventTargetFlags(cmd)
	eventFields(cmd)
	cmd.Flags().Bool("confirm", false, "confirm event update")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireEventConfirm(cmd); err != nil {
			return err
		}
		id, err := eventTarget(cmd)
		if err != nil {
			return err
		}
		c, appID, err := eventClient(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		current, err := asc.FindAppEvent(cmd.Context(), c, appID, id)
		if err != nil {
			return err
		}
		attrs, patch, err := eventInput(cmd, current.Attributes, false)
		if err != nil {
			return err
		}
		if reflect.DeepEqual(attrs, current.Attributes) {
			return Render(AppEventActionResult{Action: "skipped", ID: id, Event: &current}, outputMode())
		}
		event, changed, err := asc.UpdateAppEvent(cmd.Context(), c, appID, id, patch)
		if err != nil {
			return err
		}
		return Render(AppEventActionResult{Action: "update", ID: id, Changed: changed, Event: &event}, outputMode())
	}
	return cmd
}
func newEventsDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "delete <bundleId>", Short: "Delete a draft, archived, or approved In-App Event", Args: cobra.ExactArgs(1)}
	eventTargetFlags(cmd)
	cmd.Flags().Bool("confirm", false, "confirm event deletion")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireEventConfirm(cmd); err != nil {
			return err
		}
		id, err := eventTarget(cmd)
		if err != nil {
			return err
		}
		c, appID, err := eventClient(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if err := asc.DeleteAppEvent(cmd.Context(), c, appID, id); err != nil {
			return err
		}
		return Render(AppEventActionResult{Action: "delete", ID: id, Changed: true}, outputMode())
	}
	return cmd
}
func newEventsProposeCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "propose-submission-item <bundleId>", Short: "Check event review readiness and return an item proposal", Args: cobra.ExactArgs(1)}
	eventTargetFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		id, err := eventTarget(cmd)
		if err != nil {
			return err
		}
		c, appID, err := eventClient(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		p, err := asc.ProposeAppEventSubmissionItem(cmd.Context(), c, appID, id)
		if err != nil {
			return err
		}
		return Render(AppEventActionResult{Action: "proposal", ID: id, Proposal: &p}, outputMode())
	}
	return cmd
}
