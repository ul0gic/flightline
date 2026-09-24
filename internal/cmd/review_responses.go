package cmd

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

type ReviewResponseActionResult struct {
	Action     string `json:"action"`
	BundleID   string `json:"bundleId"`
	ReviewID   string `json:"reviewId"`
	ResponseID string `json:"responseId,omitempty"`
	Body       string `json:"responseBody,omitempty"`
	State      string `json:"state,omitempty"`
	Changed    bool   `json:"changed"`
}

func (r ReviewResponseActionResult) TableRows() (headers []string, rows [][]string) {
	return []string{"ACTION", "REVIEW_ID", "RESPONSE_ID", "STATE", "CHANGED", "BODY"}, [][]string{{r.Action, r.ReviewID, r.ResponseID, r.State, strconv.FormatBool(r.Changed), r.Body}}
}

func newReviewResponsesCommand() *cobra.Command {
	root := &cobra.Command{Use: "responses", Short: "Read, create, and delete customer review responses"}
	root.AddCommand(newReviewResponseGetCommand(), newReviewResponseCreateCommand(), newReviewResponseDeleteCommand())
	return root
}

func reviewResponseReviewFlag(cmd *cobra.Command) {
	cmd.Flags().String("review", "", "customer review ID")
}

func reviewResponseID(cmd *cobra.Command) (string, error) {
	id, _ := cmd.Flags().GetString("review")
	if id == "" {
		return "", errors.New("customer review response requires --review")
	}
	return id, nil
}

func newReviewResponseGetCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "get <bundleId>", Short: "Get the response to one app customer review", Args: cobra.ExactArgs(1)}
	reviewResponseReviewFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		reviewID, err := reviewResponseID(cmd)
		if err != nil {
			return err
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		appID, err := resolveAppID(cmd.Context(), c, args[0])
		if err != nil {
			return err
		}
		response, err := asc.ReadCustomerReviewResponse(cmd.Context(), c, appID, reviewID)
		if err != nil {
			return err
		}
		result := ReviewResponseActionResult{Action: "get", BundleID: args[0], ReviewID: reviewID}
		if response != nil {
			result.ResponseID, result.Body, result.State = response.ID, response.ResponseBody, response.State
		}
		return Render(result, outputMode())
	}
	return cmd
}

func newReviewResponseCreateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "create <bundleId>", Short: "Submit one confirmed customer review response", Args: cobra.ExactArgs(1)}
	reviewResponseReviewFlag(cmd)
	cmd.Flags().String("body", "", "exact response text")
	cmd.Flags().String("body-file", "", "file containing exact response text")
	cmd.Flags().Bool("confirm", false, "confirm response submission")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireReviewResponseConfirm(cmd); err != nil {
			return err
		}
		reviewID, err := reviewResponseID(cmd)
		if err != nil {
			return err
		}
		body, err := reviewResponseBody(cmd)
		if err != nil {
			return err
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		appID, err := resolveAppID(cmd.Context(), c, args[0])
		if err != nil {
			return err
		}
		response, changed, err := asc.CreateCustomerReviewResponse(cmd.Context(), c, appID, reviewID, body)
		if err != nil {
			return err
		}
		action := "skipped"
		if changed {
			action = "created"
		}
		return Render(ReviewResponseActionResult{Action: action, BundleID: args[0], ReviewID: reviewID, ResponseID: response.ID, Body: response.ResponseBody, State: response.State, Changed: changed}, outputMode())
	}
	return cmd
}

func newReviewResponseDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "delete <bundleId>", Short: "Delete one confirmed response by review and response ID", Args: cobra.ExactArgs(1)}
	reviewResponseReviewFlag(cmd)
	cmd.Flags().String("response", "", "expected response resource ID")
	cmd.Flags().Bool("confirm", false, "confirm response deletion")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireReviewResponseConfirm(cmd); err != nil {
			return err
		}
		reviewID, err := reviewResponseID(cmd)
		if err != nil {
			return err
		}
		responseID, _ := cmd.Flags().GetString("response")
		if responseID == "" {
			return errors.New("response deletion requires --response")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		appID, err := resolveAppID(cmd.Context(), c, args[0])
		if err != nil {
			return err
		}
		changed, err := asc.DeleteCustomerReviewResponse(cmd.Context(), c, appID, reviewID, responseID)
		if err != nil {
			return err
		}
		action := "skipped"
		if changed {
			action = "deleted"
		}
		return Render(ReviewResponseActionResult{Action: action, BundleID: args[0], ReviewID: reviewID, ResponseID: responseID, Changed: changed}, outputMode())
	}
	return cmd
}

func requireReviewResponseConfirm(cmd *cobra.Command) error {
	confirmed, _ := cmd.Flags().GetBool("confirm")
	if !confirmed {
		return errors.New("customer review response mutation requires --confirm")
	}
	return nil
}

func reviewResponseBody(cmd *cobra.Command) (string, error) {
	text, _ := cmd.Flags().GetString("body")
	file, _ := cmd.Flags().GetString("body-file")
	if cmd.Flags().Changed("body") == cmd.Flags().Changed("body-file") {
		return "", errors.New("provide exactly one of --body or --body-file")
	}
	if file != "" {
		info, err := os.Stat(file)
		if err != nil {
			return "", fmt.Errorf("read response body: %w", err)
		}
		if !info.Mode().IsRegular() {
			return "", errors.New("response body file must be regular")
		}
		data, err := os.ReadFile(file) //nolint:gosec // The user explicitly selects the response body file.
		if err != nil {
			return "", fmt.Errorf("read response body: %w", err)
		}
		text = string(data)
	}
	if strings.TrimSpace(text) == "" {
		return "", errors.New("response body is empty")
	}
	return text, nil
}
