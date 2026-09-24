package plan

import (
	"fmt"

	"github.com/ul0gic/flightline/internal/config"
)

func diffIAP(d, l *config.IAPSpec, out *[]Change) {
	if d == nil {
		return
	}
	live := map[string]config.IAPProduct{}
	if l != nil && l.Products != nil {
		live = l.Products
	}
	for _, pid := range sortedKeys(d.Products) {
		dp := d.Products[pid]
		lp, exists := live[pid]
		base := "/spec/iap/products/" + pid
		if !exists {
			*out = append(*out, Change{
				Op: OpCreate, Resource: "iap." + pid, Path: base, From: nil, To: iapParentChange(dp),
				Hint: fmt.Sprintf("create IAP %s (%s)", pid, dp.Type),
			})
			diffIAPChildren(pid, base, dp, config.IAPProduct{}, false, out)
			continue
		}
		if dp.Type != lp.Type {
			emitIfDiff(out, "iap."+pid, base+"/type", &dp.Type, &lp.Type)
		}
		emitIfDiff(out, "iap."+pid, base+"/name", dp.Name, lp.Name)
		emitIfDiff(out, "iap."+pid, base+"/familySharable", dp.FamilySharable, lp.FamilySharable)
		emitIfDiff(out, "iap."+pid, base+"/contentHosting", dp.ContentHosting, lp.ContentHosting)
		emitIfDiff(out, "iap."+pid, base+"/reviewNote", dp.ReviewNote, lp.ReviewNote)
		diffIAPChildren(pid, base, dp, lp, true, out)
	}
}

func iapParentChange(product config.IAPProduct) config.IAPProduct {
	product.Localizations = nil
	product.ReviewScreenshot = nil
	product.Commerce = nil
	return product
}

func diffIAPChildren(productID, base string, desired, live config.IAPProduct, productExists bool, out *[]Change) {
	diffIAPCommerce(productID, desired.Commerce, live.Commerce, out)
	for _, locale := range sortedKeys(desired.Localizations) {
		desiredLocalization := desired.Localizations[locale]
		liveLocalization, exists := live.Localizations[locale]
		path := base + "/localizations/" + locale
		if !productExists || !exists {
			*out = append(*out, Change{
				Op: OpCreate, Resource: "iap." + productID + ".loc." + locale,
				Path: path, From: nil, To: desiredLocalization,
				Hint: fmt.Sprintf("create IAP localization %s for %s", locale, productID),
			})
			continue
		}
		emitIfDiff(out, "iap."+productID+".loc."+locale, path+"/name", desiredLocalization.Name, liveLocalization.Name)
		emitIfDiff(out, "iap."+productID+".loc."+locale, path+"/description", desiredLocalization.Description, liveLocalization.Description)
	}
	if desired.ReviewScreenshot != nil && !equalIAPReviewScreenshot(desired.ReviewScreenshot, live.ReviewScreenshot) {
		op := OpUpdate
		if !productExists || live.ReviewScreenshot == nil {
			op = OpCreate
		}
		*out = append(*out, Change{
			Op: op, Resource: "iap." + productID + ".reviewScreenshot",
			Path: base + "/reviewScreenshot", From: live.ReviewScreenshot, To: desired.ReviewScreenshot,
			Hint: "upload IAP review screenshot for " + productID,
		})
	}
}
