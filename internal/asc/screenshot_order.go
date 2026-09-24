package asc

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

// AppScreenshot is the ordered membership projection returned by an
// AppScreenshotSet's appScreenshots relationship.
type AppScreenshot struct {
	ID                 string `json:"id"`
	SetID              string `json:"setId"`
	FileName           string `json:"fileName"`
	SourceFileChecksum string `json:"sourceFileChecksum"`
}

type appScreenshotOrderAttributes struct {
	FileName           string `json:"fileName"`
	SourceFileChecksum string `json:"sourceFileChecksum"`
}

// ListAppScreenshots returns the server's current ordered screenshot-set
// membership. The relationship endpoint establishes the target set; malformed
// or duplicate resources are rejected rather than used for a replacement.
func ListAppScreenshots(ctx context.Context, c *Client, setID string) ([]AppScreenshot, error) {
	if setID == "" {
		return nil, errors.New("app screenshot set ID is required")
	}

	out := make([]AppScreenshot, 0)
	seen := make(map[string]struct{})
	path := "/v1/appScreenshotSets/" + url.PathEscape(setID) + "/appScreenshots"
	for page, err := range Pages[appScreenshotOrderAttributes](ctx, c, path, url.Values{"limit": {"200"}}) {
		if err != nil {
			return nil, fmt.Errorf("list app screenshots for set %s: %w", setID, err)
		}
		for _, row := range page.Data {
			if row.Type != "appScreenshots" || row.ID == "" {
				return nil, fmt.Errorf("app screenshot set %s returned incomplete screenshot", setID)
			}
			if _, duplicate := seen[row.ID]; duplicate {
				return nil, fmt.Errorf("app screenshot set %s returned duplicate screenshot %s", setID, row.ID)
			}
			seen[row.ID] = struct{}{}
			out = append(out, AppScreenshot{
				ID:                 row.ID,
				SetID:              setID,
				FileName:           row.Attributes.FileName,
				SourceFileChecksum: row.Attributes.SourceFileChecksum,
			})
		}
	}
	return out, nil
}

// ReplaceAppScreenshotOrder replaces the complete ordered appScreenshots
// linkage for one already-read AppScreenshotSet. Callers must first validate
// that screenshotIDs is a complete permutation of fresh membership.
func ReplaceAppScreenshotOrder(ctx context.Context, c *Client, setID string, screenshotIDs []string) error {
	if setID == "" {
		return errors.New("app screenshot set ID is required")
	}
	if len(screenshotIDs) == 0 {
		return errors.New("app screenshot order requires at least one screenshot ID")
	}

	data := make([]appScreenshotOrderLinkage, 0, len(screenshotIDs))
	seen := make(map[string]struct{}, len(screenshotIDs))
	for _, screenshotID := range screenshotIDs {
		if screenshotID == "" {
			return errors.New("app screenshot order contains an empty screenshot ID")
		}
		if _, duplicate := seen[screenshotID]; duplicate {
			return fmt.Errorf("app screenshot order contains duplicate screenshot ID %s", screenshotID)
		}
		seen[screenshotID] = struct{}{}
		data = append(data, appScreenshotOrderLinkage{Type: "appScreenshots", ID: screenshotID})
	}

	body := appScreenshotOrderRequest{Data: data}
	path := "/v1/appScreenshotSets/" + url.PathEscape(setID) + "/relationships/appScreenshots"
	if _, err := Patch[struct{}](ctx, c, path, nil, body); err != nil {
		return fmt.Errorf("replace app screenshot order for set %s: %w", setID, err)
	}
	return nil
}

type appScreenshotOrderRequest struct {
	Data []appScreenshotOrderLinkage `json:"data"`
}

type appScreenshotOrderLinkage struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}
