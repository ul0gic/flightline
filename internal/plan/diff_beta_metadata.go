package plan

import (
	"fmt"
	"net/url"
	"sort"

	"github.com/ul0gic/flightline/internal/config"
)

func diffBetaMetadata(desired, live *config.BetaMetadataSpec, out *[]Change) {
	if desired == nil {
		return
	}
	if live == nil {
		live = &config.BetaMetadataSpec{}
	}
	for _, locale := range sortedKeys(desired.AppLocalizations) {
		want := desired.AppLocalizations[locale]
		have, exists := live.AppLocalizations[locale]
		base := "/spec/testflight/metadata/appLocalizations/" + escapeTestFlightPathSegment(locale)
		resource := "testflight.metadata.appLocalizations." + locale
		if !exists {
			*out = append(*out, Change{Op: OpCreate, Resource: resource, Path: base, To: want, Hint: "create beta app localization " + locale})
			continue
		}
		emitIfDiff(out, resource, base+"/description", want.Description, have.Description)
		emitIfDiff(out, resource, base+"/feedbackEmail", want.FeedbackEmail, have.FeedbackEmail)
		emitIfDiff(out, resource, base+"/marketingUrl", want.MarketingURL, have.MarketingURL)
		emitIfDiff(out, resource, base+"/privacyPolicyUrl", want.PrivacyPolicyURL, have.PrivacyPolicyURL)
		emitIfDiff(out, resource, base+"/tvOsPrivacyPolicy", want.TVOSPrivacyPolicy, have.TVOSPrivacyPolicy)
	}
	if desired.ReviewDetails != nil {
		want := desired.ReviewDetails
		have := &config.BetaReviewDetailsSpec{}
		if live.ReviewDetails != nil {
			have = live.ReviewDetails
		}
		base := "/spec/testflight/metadata/reviewDetails/"
		resource := "testflight.metadata.reviewDetails"
		emitIfDiff(out, resource, base+"contactFirstName", want.ContactFirstName, have.ContactFirstName)
		emitIfDiff(out, resource, base+"contactLastName", want.ContactLastName, have.ContactLastName)
		emitIfDiff(out, resource, base+"contactPhone", want.ContactPhone, have.ContactPhone)
		emitIfDiff(out, resource, base+"contactEmail", want.ContactEmail, have.ContactEmail)
		emitIfDiff(out, resource, base+"demoAccountName", want.DemoAccountName, have.DemoAccountName)
		emitIfDiff(out, resource, base+"demoAccountRequired", want.DemoAccountRequired, have.DemoAccountRequired)
		emitIfDiff(out, resource, base+"notes", want.Notes, have.Notes)
	}
	liveBuilds := make(map[config.BetaBuildSelector]config.BetaBuildMetadataSpec, len(live.Builds))
	for _, build := range live.Builds {
		liveBuilds[build.Build] = build
	}
	builds := append([]config.BetaBuildMetadataSpec(nil), desired.Builds...)
	sort.Slice(builds, func(i, j int) bool { return betaBuildKey(builds[i].Build) < betaBuildKey(builds[j].Build) })
	for _, build := range builds {
		liveBuild := liveBuilds[build.Build]
		buildKey := betaBuildKey(build.Build)
		for _, locale := range sortedKeys(build.Localizations) {
			want := build.Localizations[locale]
			have, exists := liveBuild.Localizations[locale]
			base := "/spec/testflight/metadata/builds/" + escapeTestFlightPathSegment(buildKey) + "/localizations/" + escapeTestFlightPathSegment(locale)
			resource := "testflight.metadata.builds." + buildKey
			if !exists {
				*out = append(*out, Change{Op: OpCreate, Resource: resource, Path: base, To: want, Hint: fmt.Sprintf("create beta build %s localization %s", buildKey, locale)})
				continue
			}
			emitIfDiff(out, resource, base+"/whatsNew", want.WhatsNew, have.WhatsNew)
		}
	}
}

func betaBuildKey(selector config.BetaBuildSelector) string {
	return url.QueryEscape(selector.Platform) + ":" + url.QueryEscape(selector.Version) + ":" + url.QueryEscape(selector.Number)
}
