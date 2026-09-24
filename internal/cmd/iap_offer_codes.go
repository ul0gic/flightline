package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

type IAPOfferCodesResult struct {
	ProductID string             `json:"productId"`
	Offers    []asc.IAPOfferCode `json:"offers"`
}

func (r IAPOfferCodesResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"ID", "NAME", "PRODUCTION_CODES", "SANDBOX_CODES"}
	rows = make([][]string, 0, len(r.Offers))
	for _, offer := range r.Offers {
		rows = append(rows, []string{offer.ID, offer.Name, strconv.Itoa(offer.ProductionCodeCount), strconv.Itoa(offer.SandboxCodeCount)})
	}
	return headers, rows
}

type IAPOfferCodeDetailResult struct {
	ProductID string                 `json:"productId"`
	Detail    asc.IAPOfferCodeDetail `json:"detail"`
}

func (r IAPOfferCodeDetailResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"ID", r.Detail.ID}, {"NAME", r.Detail.Name},
		{"PRICES", strconv.Itoa(len(r.Detail.Prices))},
		{"CUSTOM_BATCHES", strconv.Itoa(len(r.Detail.CustomBatches))},
		{"ONE_TIME_BATCHES", strconv.Itoa(len(r.Detail.OneTimeBatches))},
	}
	return headers, rows
}

type IAPOfferActionResult struct {
	Action      string `json:"action"`
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Count       int    `json:"count,omitempty"`
	Environment string `json:"environment,omitempty"`
	File        string `json:"file,omitempty"`
	Bytes       int64  `json:"bytes,omitempty"`
}

func (r IAPOfferActionResult) TableRows() (headers []string, rows [][]string) {
	return []string{"ACTION", "ID", "NAME", "COUNT", "ENVIRONMENT", "FILE", "BYTES"}, [][]string{{
		r.Action, r.ID, r.Name, strconv.Itoa(r.Count), r.Environment, r.File, strconv.FormatInt(r.Bytes, 10),
	}}
}

func newIAPOfferCodesCommand() *cobra.Command {
	root := &cobra.Command{Use: "offer-codes", Short: "Inspect and deliberately issue non-subscription IAP offer codes"}
	root.AddCommand(newIAPOffersListCommand(), newIAPOffersGetCommand(), newIAPOfferDefinitionCreateCommand(),
		newIAPOfferCustomCreateCommand(), newIAPOfferOneTimeCreateCommand(), newIAPOfferValuesDownloadCommand())
	return root
}

func newIAPOffersListCommand() *cobra.Command {
	cmd := newIAPOfferReadCommand("list <bundleId>", "List offer definitions for one IAP")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, iapID, productID, err := offerIAP(cmd, args[0])
		if err != nil {
			return err
		}
		offers, err := asc.ListIAPOfferCodes(cmd.Context(), c, iapID)
		if err != nil {
			return err
		}
		return Render(IAPOfferCodesResult{ProductID: productID, Offers: offers}, outputMode())
	}
	return cmd
}

func newIAPOffersGetCommand() *cobra.Command {
	cmd := newIAPOfferReadCommand("get <bundleId>", "Inspect an offer and its complete prices and batches")
	cmd.Flags().String("offer", "", "offer definition ID")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, iapID, productID, err := offerIAP(cmd, args[0])
		if err != nil {
			return err
		}
		offerID, err := requiredOfferID(cmd)
		if err != nil {
			return err
		}
		detail, err := asc.GetIAPOfferCode(cmd.Context(), c, iapID, offerID)
		if err != nil {
			return err
		}
		return Render(IAPOfferCodeDetailResult{ProductID: productID, Detail: detail}, outputMode())
	}
	return cmd
}

func newIAPOfferDefinitionCreateCommand() *cobra.Command {
	cmd := newIAPOfferReadCommand("create-definition <bundleId>", "Create an IAP offer definition with explicit prices")
	cmd.Flags().String("name", "", "offer name")
	cmd.Flags().StringArray("eligibility", nil, "customer eligibility; repeat NON_SPENDER, ACTIVE_SPENDER, or CHURNED_SPENDER")
	cmd.Flags().StringArray("price", nil, "territory=IAP price-point ID; repeat per territory")
	cmd.Flags().Bool("confirm", false, "confirm offer definition creation")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireOfferConfirm(cmd); err != nil {
			return err
		}
		name, _ := cmd.Flags().GetString("name")
		eligibility, _ := cmd.Flags().GetStringArray("eligibility")
		prices, _ := cmd.Flags().GetStringArray("price")
		choices, err := parseIAPOfferPriceChoices(prices)
		if err != nil {
			return err
		}
		c, iapID, _, err := offerIAP(cmd, args[0])
		if err != nil {
			return err
		}
		created, err := asc.CreateIAPOfferCode(cmd.Context(), c, iapID, name, eligibility, choices)
		if err != nil {
			return err
		}
		return Render(IAPOfferActionResult{Action: "create-definition", ID: created.ID, Name: created.Name}, outputMode())
	}
	return cmd
}

func newIAPOfferCustomCreateCommand() *cobra.Command {
	cmd := newIAPOfferReadCommand("create-custom <bundleId>", "Create a custom code batch for an existing offer")
	cmd.Flags().String("offer", "", "offer definition ID")
	cmd.Flags().String("code-file", "", "0600 file containing the custom code")
	cmd.Flags().Int("count", 0, "number of codes")
	cmd.Flags().String("expires", "", "optional future expiration date, YYYY-MM-DD")
	cmd.Flags().Bool("confirm", false, "confirm custom code creation")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireOfferConfirm(cmd); err != nil {
			return err
		}
		offerID, err := requiredOfferID(cmd)
		if err != nil {
			return err
		}
		file, _ := cmd.Flags().GetString("code-file")
		code, err := readIAPCustomCodeFile(file)
		if err != nil {
			return err
		}
		count, _ := cmd.Flags().GetInt("count")
		expires, _ := cmd.Flags().GetString("expires")
		c, iapID, _, err := offerIAP(cmd, args[0])
		if err != nil {
			return err
		}
		created, err := asc.CreateIAPCustomCodeBatch(cmd.Context(), c, iapID, offerID, code, count, expires, time.Now().UTC())
		if err != nil {
			return err
		}
		return Render(IAPOfferActionResult{Action: "create-custom", ID: created.ID, Count: created.NumberOfCodes}, outputMode())
	}
	return cmd
}

func newIAPOfferOneTimeCreateCommand() *cobra.Command {
	cmd := newIAPOfferReadCommand("create-one-time <bundleId>", "Issue a one-time code batch for an existing offer")
	cmd.Flags().String("offer", "", "offer definition ID")
	cmd.Flags().Int("count", 0, "number of codes")
	cmd.Flags().String("expires", "", "future expiration date, YYYY-MM-DD")
	cmd.Flags().String("environment", "", "PRODUCTION or SANDBOX")
	cmd.Flags().Bool("confirm", false, "confirm one-time batch issuance")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireOfferConfirm(cmd); err != nil {
			return err
		}
		offerID, err := requiredOfferID(cmd)
		if err != nil {
			return err
		}
		count, _ := cmd.Flags().GetInt("count")
		expires, _ := cmd.Flags().GetString("expires")
		environment, _ := cmd.Flags().GetString("environment")
		c, iapID, _, err := offerIAP(cmd, args[0])
		if err != nil {
			return err
		}
		created, err := asc.CreateIAPOneTimeCodeBatch(cmd.Context(), c, iapID, offerID, count, expires, environment, time.Now().UTC())
		if err != nil {
			return err
		}
		return Render(IAPOfferActionResult{Action: "create-one-time", ID: created.ID, Count: created.NumberOfCodes, Environment: created.Environment}, outputMode())
	}
	return cmd
}

func newIAPOfferValuesDownloadCommand() *cobra.Command {
	cmd := newIAPOfferReadCommand("download <bundleId>", "Download one-time code values to a new private CSV file")
	cmd.Flags().String("offer", "", "offer definition ID")
	cmd.Flags().String("batch", "", "one-time batch ID")
	cmd.Flags().String("file", "", "new destination CSV path; existing files are never overwritten")
	cmd.Flags().Bool("confirm", false, "confirm code value download")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireOfferConfirm(cmd); err != nil {
			return err
		}
		offerID, err := requiredOfferID(cmd)
		if err != nil {
			return err
		}
		batchID, _ := cmd.Flags().GetString("batch")
		file, _ := cmd.Flags().GetString("file")
		if batchID == "" || file == "" {
			return errors.New("download requires --batch and --file")
		}
		c, iapID, _, err := offerIAP(cmd, args[0])
		if err != nil {
			return err
		}
		bytesWritten, err := downloadIAPOfferValues(cmd, c, iapID, offerID, batchID, file)
		if err != nil {
			return err
		}
		return Render(IAPOfferActionResult{Action: "download", ID: batchID, File: file, Bytes: bytesWritten}, outputMode())
	}
	return cmd
}

func newIAPOfferReadCommand(use, short string) *cobra.Command {
	cmd := &cobra.Command{Use: use, Short: short, Args: cobra.ExactArgs(1), SilenceUsage: true}
	cmd.Flags().String("product", "", "IAP product ID")
	return cmd
}

func offerIAP(cmd *cobra.Command, bundleID string) (client *asc.Client, iapID, productID string, err error) {
	productID, _ = cmd.Flags().GetString("product")
	if productID == "" {
		return nil, "", "", errors.New("offer-codes requires --product")
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

func requiredOfferID(cmd *cobra.Command) (string, error) {
	id, _ := cmd.Flags().GetString("offer")
	if id == "" {
		return "", errors.New("--offer is required")
	}
	return id, nil
}

func requireOfferConfirm(cmd *cobra.Command) error {
	confirmed, _ := cmd.Flags().GetBool("confirm")
	if !confirmed {
		return errors.New("offer-code mutation or download requires --confirm")
	}
	return nil
}

func parseIAPOfferPriceChoices(values []string) ([]asc.IAPOfferPriceChoice, error) {
	choices := make([]asc.IAPOfferPriceChoice, 0, len(values))
	for _, value := range values {
		territory, point, ok := strings.Cut(value, "=")
		if !ok || territory == "" || point == "" {
			return nil, errors.New("--price must be territory=IAP-price-point-ID")
		}
		choices = append(choices, asc.IAPOfferPriceChoice{TerritoryID: territory, PricePointID: point})
	}
	return choices, nil
}

func readIAPCustomCodeFile(path string) (string, error) {
	if path == "" {
		return "", errors.New("--code-file is required")
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()
	file, err := root.Open(filepath.Base(path))
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("custom code file must be a private regular file (0600)")
	}
	content, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return "", err
	}
	if len(content) > 4096 {
		return "", errors.New("custom code file exceeds 4096 bytes")
	}
	code := strings.TrimRight(string(content), "\r\n")
	if code == "" || strings.ContainsAny(code, "\r\n") {
		return "", errors.New("custom code file must contain exactly one nonempty line")
	}
	return code, nil
}

func downloadIAPOfferValues(cmd *cobra.Command, c *asc.Client, iapID, offerID, batchID, path string) (int64, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return 0, err
	}
	defer func() { _ = root.Close() }()
	name := filepath.Base(path)
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 0, fmt.Errorf("create private CSV: %w", err)
	}
	remove := true
	defer func() {
		if remove {
			_ = root.Remove(name)
		}
	}()
	count, err := asc.DownloadIAPOneTimeCodes(cmd.Context(), c, iapID, offerID, batchID, file)
	if err != nil {
		_ = file.Close()
		return 0, err
	}
	if count == 0 {
		_ = file.Close()
		return 0, errors.New("empty code values response")
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return 0, err
	}
	if err := file.Close(); err != nil {
		return 0, err
	}
	remove = false
	return count, nil
}
