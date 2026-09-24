package cmd

import (
	"errors"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

type WebhookDeliveriesResult struct {
	WebhookID  string                `json:"webhookId"`
	Deliveries []asc.WebhookDelivery `json:"deliveries"`
}

func (r WebhookDeliveriesResult) TableRows() (headers []string, rows [][]string) {
	rows = make([][]string, 0, len(r.Deliveries))
	for _, d := range r.Deliveries {
		status := ""
		if d.StatusCode != nil {
			status = strconv.Itoa(*d.StatusCode)
		}
		rows = append(rows, []string{d.ID, d.State, d.CreatedDate, d.SentDate, status, d.Endpoint})
	}
	return []string{"ID", "STATE", "CREATED", "SENT", "HTTP_STATUS", "ENDPOINT"}, rows
}

type WebhookPingResult struct {
	WebhookID string `json:"webhookId"`
	PingID    string `json:"pingId"`
}

func (r WebhookPingResult) TableRows() (headers []string, rows [][]string) {
	return []string{"WEBHOOK_ID", "PING_ID"}, [][]string{{r.WebhookID, r.PingID}}
}

type WebhookRedeliveryResult struct {
	WebhookID  string              `json:"webhookId"`
	TemplateID string              `json:"templateId"`
	Delivery   asc.WebhookDelivery `json:"delivery"`
}

func (r WebhookRedeliveryResult) TableRows() (headers []string, rows [][]string) {
	return []string{"WEBHOOK_ID", "TEMPLATE_ID", "NEW_DELIVERY_ID", "STATE"}, [][]string{{r.WebhookID, r.TemplateID, r.Delivery.ID, r.Delivery.State}}
}

func newWebhookPingCommand() *cobra.Command {
	var confirm bool
	cmd := &cobra.Command{Use: "ping <bundleId> <webhookId>", Short: "Request a webhook ping", Args: cobra.ExactArgs(2), SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			return runWebhookPing(cmd, c, args[0], args[1], confirm, outputMode())
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "confirm an outbound webhook ping")
	return cmd
}

func newWebhookDeliveriesCommand() *cobra.Command {
	group := &cobra.Command{Use: "deliveries", Short: "Inspect webhook deliveries and request explicit redelivery"}
	group.AddCommand(&cobra.Command{Use: "list <bundleId> <webhookId>", Short: "List delivery attempts for an app webhook", Args: cobra.ExactArgs(2), SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			return runWebhookDeliveriesList(cmd, c, args[0], args[1], outputMode())
		},
	})
	var confirm bool
	redeliver := &cobra.Command{Use: "redeliver <bundleId> <webhookId> <deliveryId>", Short: "Request a confirmed retry of an owned delivery", Args: cobra.ExactArgs(3), SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			return runWebhookRedelivery(cmd, c, args[0], args[1], args[2], confirm, outputMode())
		},
	}
	redeliver.Flags().BoolVar(&confirm, "confirm", false, "confirm outbound delivery retry")
	group.AddCommand(redeliver)
	return group
}

func runWebhookDeliveriesList(cmd *cobra.Command, c *asc.Client, bundleID, webhookID, output string) error {
	appID, err := resolveWebhookApp(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}
	if _, err := webhookOwned(cmd.Context(), c, appID, webhookID); err != nil {
		return err
	}
	rows, err := asc.ListWebhookDeliveries(cmd.Context(), c, webhookID)
	if err != nil {
		return err
	}
	return renderTo(cmd.OutOrStdout(), WebhookDeliveriesResult{WebhookID: webhookID, Deliveries: rows}, output, true)
}

func runWebhookPing(cmd *cobra.Command, c *asc.Client, bundleID, webhookID string, confirm bool, output string) error {
	if !confirm {
		return errors.New("webhook ping requires --confirm")
	}
	appID, err := resolveWebhookApp(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}
	if _, err := webhookOwned(cmd.Context(), c, appID, webhookID); err != nil {
		return err
	}
	id, err := asc.PingWebhook(cmd.Context(), c, webhookID)
	if err != nil {
		return err
	}
	return renderTo(cmd.OutOrStdout(), WebhookPingResult{WebhookID: webhookID, PingID: id}, output, true)
}

func runWebhookRedelivery(cmd *cobra.Command, c *asc.Client, bundleID, webhookID, deliveryID string, confirm bool, output string) error {
	if !confirm {
		return errors.New("webhook redelivery requires --confirm")
	}
	appID, err := resolveWebhookApp(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}
	if _, err := webhookOwned(cmd.Context(), c, appID, webhookID); err != nil {
		return err
	}
	rows, err := asc.ListWebhookDeliveries(cmd.Context(), c, webhookID)
	if err != nil {
		return err
	}
	if !containsWebhookDelivery(rows, deliveryID) {
		return errors.New("delivery is not owned by the selected webhook")
	}
	newDelivery, err := asc.RedeliverWebhookDelivery(cmd.Context(), c, deliveryID)
	if err != nil {
		return err
	}
	return renderTo(cmd.OutOrStdout(), WebhookRedeliveryResult{WebhookID: webhookID, TemplateID: deliveryID, Delivery: newDelivery}, output, true)
}

func containsWebhookDelivery(rows []asc.WebhookDelivery, id string) bool {
	for _, row := range rows {
		if row.ID == id {
			return true
		}
	}
	return false
}
