package cmd

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

type IAPPricePointsResult struct {
	ProductID string              `json:"productId"`
	Points    []asc.IAPPricePoint `json:"pricePoints"`
}

func (r IAPPricePointsResult) TableRows() (headers []string, rows [][]string) {
	rows = make([][]string, 0, len(r.Points))
	for _, point := range r.Points {
		rows = append(rows, []string{point.ID, point.TerritoryID, point.CustomerPrice, point.Proceeds})
	}
	return []string{"PRICE_POINT_ID", "TERRITORY", "CUSTOMER_PRICE", "PROCEEDS"}, rows
}

type IAPPricingResult struct {
	ProductID string               `json:"productId"`
	Schedule  asc.IAPPriceSchedule `json:"schedule"`
}

func (r IAPPricingResult) TableRows() (headers []string, rows [][]string) {
	rows = make([][]string, 0, len(r.Schedule.ManualPrices)+1)
	rows = append(rows, []string{"schedule", r.Schedule.ID, r.Schedule.BaseTerritoryID, "", ""})
	for _, price := range r.Schedule.ManualPrices {
		rows = append(rows, []string{price.ID, price.PricePointID, price.TerritoryID, price.StartDate, price.EndDate})
	}
	return []string{"PRICE_ID", "PRICE_POINT_ID", "TERRITORY", "START_DATE", "END_DATE"}, rows
}

type IAPAvailabilityResult struct {
	ProductID    string              `json:"productId"`
	Availability asc.IAPAvailability `json:"availability"`
}

type IAPCommerceWriteResult struct {
	Action    string `json:"action"`
	ProductID string `json:"productId"`
	ID        string `json:"id"`
	Changed   bool   `json:"changed"`
}

func (r IAPCommerceWriteResult) TableRows() (headers []string, rows [][]string) {
	return []string{"ACTION", "PRODUCT_ID", "ID", "CHANGED"}, [][]string{{r.Action, r.ProductID, r.ID, strconv.FormatBool(r.Changed)}}
}

func (r IAPAvailabilityResult) TableRows() (headers []string, rows [][]string) {
	value := "(none)"
	if r.Availability.AvailableInNewTerritories != nil {
		value = strconv.FormatBool(*r.Availability.AvailableInNewTerritories)
	}
	return []string{"ID", "NEW_TERRITORIES", "AVAILABLE_TERRITORIES"}, [][]string{{r.Availability.ID, value, strings.Join(r.Availability.TerritoryIDs, ",")}}
}

func newIAPCommerceCommand() *cobra.Command {
	root := &cobra.Command{Use: "commerce", Short: "Inspect and set non-subscription IAP pricing and availability"}
	root.AddCommand(newIAPPricePointsCommand(), newIAPPricingCommand(), newIAPAvailabilityCommand(), newIAPSetPriceCommand(), newIAPSetAvailabilityCommand())
	return root
}

func newIAPSetPriceCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "set-price <bundleId>", Short: "Set the current IAP base price while preserving manual windows", Args: cobra.ExactArgs(1)}
	cmd.Flags().String("product", "", "IAP product ID")
	cmd.Flags().String("base-territory", "", "base territory ID")
	cmd.Flags().String("price-point", "", "IAP price point ID in the base territory")
	cmd.Flags().Bool("confirm", false, "confirm IAP price change")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireIAPCommerceConfirm(cmd); err != nil {
			return err
		}
		territory, _ := cmd.Flags().GetString("base-territory")
		point, _ := cmd.Flags().GetString("price-point")
		if territory == "" || point == "" {
			return errors.New("set-price requires --base-territory and --price-point")
		}
		c, iapID, productID, err := commerceIAP(cmd, args[0])
		if err != nil {
			return err
		}
		result, err := setIAPPriceWithClient(cmd.Context(), c, iapID, territory, point, time.Now().UTC())
		if err != nil {
			return err
		}
		return Render(IAPCommerceWriteResult{Action: "set-price", ProductID: productID, ID: result.ID, Changed: result.Changed}, outputMode())
	}
	return cmd
}

func setIAPPriceWithClient(ctx context.Context, c *asc.Client, iapID, territory, point string, at time.Time) (asc.IAPCommerceWriteResult, error) {
	schedule, err := asc.ReadIAPPriceSchedule(ctx, c, iapID)
	if err != nil {
		return asc.IAPCommerceWriteResult{}, err
	}
	current, err := schedule.ActiveBasePrice(at)
	if err != nil {
		return asc.IAPCommerceWriteResult{}, err
	}
	var expected *asc.IAPBasePrice
	if current.PricePointID != "" {
		expected = &current
	}
	return asc.CreatePreservingIAPPriceSchedule(ctx, c, iapID, territory, point, expected, at)
}

func newIAPSetAvailabilityCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "set-availability <bundleId>", Short: "Set IAP territory availability as an explicit complete set", Args: cobra.ExactArgs(1)}
	cmd.Flags().String("product", "", "IAP product ID")
	cmd.Flags().Bool("new-territories", false, "make the IAP available in newly added territories")
	cmd.Flags().StringArray("territory", nil, "available territory ID; repeat for the complete set")
	cmd.Flags().Bool("clear-territories", false, "explicitly clear all available territories")
	cmd.Flags().Bool("confirm", false, "confirm IAP availability change")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireIAPCommerceConfirm(cmd); err != nil {
			return err
		}
		input, err := iapAvailabilityFlags(cmd)
		if err != nil {
			return err
		}
		c, iapID, productID, err := commerceIAP(cmd, args[0])
		if err != nil {
			return err
		}
		result, err := setIAPAvailabilityWithClient(cmd.Context(), c, iapID, input)
		if err != nil {
			return err
		}
		return Render(IAPCommerceWriteResult{Action: "set-availability", ProductID: productID, ID: result.ID, Changed: result.Changed}, outputMode())
	}
	return cmd
}

type iapAvailabilityInput struct {
	newTerritories *bool
	territories    *[]string
}

func iapAvailabilityFlags(cmd *cobra.Command) (iapAvailabilityInput, error) {
	var input iapAvailabilityInput
	if cmd.Flags().Changed("new-territories") {
		value, _ := cmd.Flags().GetBool("new-territories")
		input.newTerritories = &value
	}
	territories, _ := cmd.Flags().GetStringArray("territory")
	clearTerritories, _ := cmd.Flags().GetBool("clear-territories")
	if clearTerritories && cmd.Flags().Changed("territory") {
		return input, errors.New("--clear-territories cannot be combined with --territory")
	}
	if clearTerritories {
		empty := []string{}
		input.territories = &empty
	} else if cmd.Flags().Changed("territory") {
		seen := make(map[string]bool)
		for _, territory := range territories {
			if territory == "" || seen[territory] {
				return input, errors.New("--territory requires nonempty unique IDs")
			}
			seen[territory] = true
		}
		input.territories = &territories
	}
	if input.newTerritories == nil && input.territories == nil {
		return input, errors.New("set-availability requires --new-territories, --territory, or --clear-territories")
	}
	return input, nil
}

func setIAPAvailabilityWithClient(ctx context.Context, c *asc.Client, iapID string, input iapAvailabilityInput) (asc.IAPCommerceWriteResult, error) {
	current, err := asc.ReadIAPAvailability(ctx, c, iapID)
	if err != nil {
		return asc.IAPCommerceWriteResult{}, err
	}
	var expected *asc.IAPAvailability
	if current.ID != "" {
		expected = &current
	}
	newTerritories := input.newTerritories
	territories := input.territories
	if expected != nil {
		if newTerritories == nil {
			newTerritories = current.AvailableInNewTerritories
		}
		if territories == nil {
			territories = &current.TerritoryIDs
		}
	}
	if newTerritories == nil || territories == nil {
		return asc.IAPCommerceWriteResult{}, errors.New("initial availability requires --new-territories and an explicit territory set")
	}
	return asc.CreateIAPAvailability(ctx, c, iapID, *newTerritories, *territories, expected)
}

func requireIAPCommerceConfirm(cmd *cobra.Command) error {
	confirm, _ := cmd.Flags().GetBool("confirm")
	if !confirm {
		return errors.New("IAP commerce write requires --confirm")
	}
	return nil
}

func newIAPPricePointsCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "price-points <bundleId>", Short: "List price points for one IAP", Args: cobra.ExactArgs(1)}
	cmd.Flags().String("product", "", "IAP product ID")
	cmd.Flags().String("territory", "", "optional territory ID filter")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, iapID, productID, err := commerceIAP(cmd, args[0])
		if err != nil {
			return err
		}
		territory, _ := cmd.Flags().GetString("territory")
		points, err := asc.ListIAPPricePoints(cmd.Context(), c, iapID, territory)
		if err != nil {
			return err
		}
		return Render(IAPPricePointsResult{ProductID: productID, Points: points}, outputMode())
	}
	return cmd
}

func newIAPPricingCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "pricing <bundleId>", Short: "Inspect complete IAP manual price windows", Args: cobra.ExactArgs(1)}
	cmd.Flags().String("product", "", "IAP product ID")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, iapID, productID, err := commerceIAP(cmd, args[0])
		if err != nil {
			return err
		}
		schedule, err := asc.ReadIAPPriceSchedule(cmd.Context(), c, iapID)
		if err != nil {
			return err
		}
		return Render(IAPPricingResult{ProductID: productID, Schedule: schedule}, outputMode())
	}
	return cmd
}

func newIAPAvailabilityCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "availability <bundleId>", Short: "Inspect IAP territory availability", Args: cobra.ExactArgs(1)}
	cmd.Flags().String("product", "", "IAP product ID")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, iapID, productID, err := commerceIAP(cmd, args[0])
		if err != nil {
			return err
		}
		availability, err := asc.ReadIAPAvailability(cmd.Context(), c, iapID)
		if err != nil {
			return err
		}
		return Render(IAPAvailabilityResult{ProductID: productID, Availability: availability}, outputMode())
	}
	return cmd
}

func commerceIAP(cmd *cobra.Command, bundleID string) (client *asc.Client, iapID, productID string, err error) {
	productID, _ = cmd.Flags().GetString("product")
	if productID == "" {
		return nil, "", "", errors.New("iap commerce requires --product")
	}
	c, err := newClient()
	if err != nil {
		return nil, "", "", err
	}
	var attributes asc.IAPAttributes
	iapID, attributes, err = findIAPByProductID(cmd.Context(), c, bundleID, productID)
	if err != nil {
		return nil, "", "", err
	}
	if iapID == "" || attributes.ProductID != productID {
		return nil, "", "", errors.New("IAP product lookup did not confirm requested product ID")
	}
	return c, iapID, productID, nil
}
