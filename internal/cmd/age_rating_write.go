package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
	"go.yaml.in/yaml/v3"
)

// AgeRatingWriteResult is the JSON-stable view returned by `age-rating set`.
// noop=true means current state already matched the supplied payload —
// idempotency contract.
type AgeRatingWriteResult struct {
	Action       string                             `json:"action"`
	ID           string                             `json:"id"`
	Type         string                             `json:"type"`
	BundleID     string                             `json:"bundleId"`
	Version      string                             `json:"version"`
	VersionState string                             `json:"versionState,omitempty"`
	NoOp         bool                               `json:"noop"`
	ChangedKeys  []string                           `json:"changedKeys,omitempty"`
	Attributes   asc.AgeRatingDeclarationAttributes `json:"attributes"`
}

// TableRows for AgeRatingWriteResult — vertical layout, identifying fields up
// top, then changed keys, then a snapshot of attributes.
func (r *AgeRatingWriteResult) TableRows() (headers []string, rows [][]string) {
	headers = []string{"FIELD", "VALUE"}
	rows = [][]string{
		{"ACTION", r.Action},
		{"ID", r.ID},
		{"TYPE", r.Type},
		{"BUNDLE_ID", r.BundleID},
		{"VERSION", r.Version},
		{"VERSION_STATE", r.VersionState},
		{"NOOP", fmt.Sprintf("%t", r.NoOp)},
		{"CHANGED_KEYS", strings.Join(r.ChangedKeys, ",")},
	}
	return headers, rows
}

// ageRatingPatchRequest is the wire body for PATCH
// /v1/ageRatingDeclarations/{id}. Mirrors AgeRatingDeclarationUpdateRequest.
type ageRatingPatchRequest struct {
	Data ageRatingPatchData `json:"data"`
}

type ageRatingPatchData struct {
	Type       string         `json:"type"`
	ID         string         `json:"id"`
	Attributes map[string]any `json:"attributes"`
}

var ageRatingSetCmd = &cobra.Command{
	Use:          "set <bundleId>",
	Short:        "Set age-rating questionnaire answers from a YAML/JSON payload (idempotent)",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runAgeRatingSet,
	Long: `set PATCHes the age-rating declaration for a version with the answers loaded
from --from. Accepts JSON (.json) or YAML (.yaml/.yml). Top-level keys must
match Apple's wire names (e.g. ` + "`alcoholTobaccoOrDrugUseOrReferences: NONE`" + `).

Idempotent: the file is diffed against the current declaration; only fields
that actually differ go in the PATCH body. When everything already matches,
returns noop=true without issuing a PATCH.

Validation: every key must be a recognized field on the declaration; an
unknown key surfaces as a typed error naming the offending key. Frequency
enums must be one of NONE | INFREQUENT_OR_MILD | FREQUENT_OR_INTENSE |
INFREQUENT | FREQUENT. Boolean fields must be true/false.`,
	Example: `  fline age-rating set com.example.myapp --version 1.0.1 --from age-rating.yaml
  fline age-rating set com.example.myapp --version 1.0.1 --from age-rating.json --output json`,
}

var (
	ageRatingSetVersion  string
	ageRatingSetPlatform string
	ageRatingSetFrom     string
)

func init() {
	ageRatingSetCmd.Flags().StringVar(&ageRatingSetVersion, "version", "", "version string to look up (e.g. 1.0.1)")
	ageRatingSetCmd.Flags().StringVar(&ageRatingSetPlatform, "platform", "IOS", "platform (IOS|MAC_OS|TV_OS|VISION_OS)")
	ageRatingSetCmd.Flags().StringVar(&ageRatingSetFrom, "from", "", "path to YAML/JSON file containing the questionnaire answers")
	_ = ageRatingSetCmd.MarkFlagRequired("version")
	_ = ageRatingSetCmd.MarkFlagRequired("from")

	ageRatingCmd.AddCommand(ageRatingSetCmd)
}

func runAgeRatingSet(cmd *cobra.Command, args []string) error {
	bundleID := args[0]
	versionStr := strings.TrimSpace(ageRatingSetVersion)
	platform := strings.TrimSpace(ageRatingSetPlatform)
	from := strings.TrimSpace(ageRatingSetFrom)

	c, err := newClient()
	if err != nil {
		return err
	}

	// Load + validate the payload BEFORE we hit the API. Surface bad input
	// with file path + key context so users edit the right line.
	payload, err := loadAgeRatingPayload(from)
	if err != nil {
		return err
	}
	desired, err := payload.toAttributes()
	if err != nil {
		return fmt.Errorf("age-rating set: %s: %w", from, err)
	}
	if err := validateAgeRatingAttributes(desired); err != nil {
		return fmt.Errorf("age-rating set: %s: %w", from, err)
	}

	// Resolve version → appInfo → ageRatingDeclaration ID.
	appID, err := resolveAppID(cmd.Context(), c, bundleID)
	if err != nil {
		return err
	}
	versionState, err := lookupVersionState(cmd.Context(), c, appID, versionStr, platform)
	if err != nil {
		return err
	}
	appInfoID, err := pickAppInfoForVersion(cmd.Context(), c, appID, versionState)
	if err != nil {
		return err
	}
	current, declID, err := fetchAgeRatingDeclaration(cmd.Context(), c, appInfoID)
	if err != nil {
		return err
	}

	// Diff: build a map of changed-only fields.
	changes := diffAgeRating(current, desired, payload.providedKeys())
	if len(changes) == 0 {
		return Render(&AgeRatingWriteResult{
			Action:       "set",
			ID:           declID,
			Type:         "ageRatingDeclarations",
			BundleID:     bundleID,
			Version:      versionStr,
			VersionState: versionState,
			NoOp:         true,
			Attributes:   current,
		}, outputMode())
	}

	body := ageRatingPatchRequest{
		Data: ageRatingPatchData{
			Type:       "ageRatingDeclarations",
			ID:         declID,
			Attributes: changes,
		},
	}
	resp, err := asc.Patch[asc.Single[asc.AgeRatingDeclarationAttributes]](
		cmd.Context(), c, "/v1/ageRatingDeclarations/"+declID, nil, body,
	)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(changes))
	for k := range changes {
		keys = append(keys, k)
	}
	sortStrings(keys)
	return Render(&AgeRatingWriteResult{
		Action:       "set",
		ID:           resp.Data.ID,
		Type:         resp.Data.Type,
		BundleID:     bundleID,
		Version:      versionStr,
		VersionState: versionState,
		NoOp:         false,
		ChangedKeys:  keys,
		Attributes:   resp.Data.Attributes,
	}, outputMode())
}

// ----------------------------------------------------------------------------
// Payload loading + validation
// ----------------------------------------------------------------------------

// ageRatingPayload is the parsed --from file. raw holds the original key/value
// map so we can distinguish "user set this to empty/false" from "user did not
// supply this key" — the difference between an explicit clear and a leave-
// alone in idempotent diffs.
type ageRatingPayload struct {
	raw map[string]any
}

// providedKeys returns the wire keys the user explicitly set in the file.
// Sorted for deterministic test output.
func (p *ageRatingPayload) providedKeys() map[string]struct{} {
	out := make(map[string]struct{}, len(p.raw))
	for k := range p.raw {
		out[k] = struct{}{}
	}
	return out
}

// loadAgeRatingPayload reads --from and decodes JSON or YAML based on the
// file extension. Empty file is rejected — callers shouldn't run a set with
// nothing to say.
func loadAgeRatingPayload(path string) (*ageRatingPayload, error) {
	if path == "" {
		return nil, errors.New("age-rating set: --from is required")
	}
	buf, err := os.ReadFile(path) //nolint:gosec // path is a user-supplied flag value
	if err != nil {
		return nil, fmt.Errorf("age-rating set: read %s: %w", path, err)
	}
	if strings.TrimSpace(string(buf)) == "" {
		return nil, fmt.Errorf("age-rating set: %s is empty", path)
	}

	raw := map[string]any{}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		if err := json.Unmarshal(buf, &raw); err != nil {
			return nil, fmt.Errorf("age-rating set: parse %s as JSON: %w", path, err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(buf, &raw); err != nil {
			return nil, fmt.Errorf("age-rating set: parse %s as YAML: %w", path, err)
		}
	default:
		// Try JSON first then YAML — neither extension match.
		if jerr := json.Unmarshal(buf, &raw); jerr != nil {
			if yerr := yaml.Unmarshal(buf, &raw); yerr != nil {
				return nil, fmt.Errorf("age-rating set: %s: not parseable as JSON or YAML: %w", path, yerr)
			}
		}
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("age-rating set: %s decoded to an empty object — at least one questionnaire key required", path)
	}
	return &ageRatingPayload{raw: raw}, nil
}

// toAttributes round-trips the raw map through JSON into the typed
// AgeRatingDeclarationAttributes struct. Unknown wire keys are caught here
// (json.DisallowUnknownFields) so the user sees what they typo'd.
func (p *ageRatingPayload) toAttributes() (asc.AgeRatingDeclarationAttributes, error) {
	if err := assertKnownAgeRatingKeys(p.raw); err != nil {
		return asc.AgeRatingDeclarationAttributes{}, err
	}
	buf, err := json.Marshal(p.raw)
	if err != nil {
		return asc.AgeRatingDeclarationAttributes{}, fmt.Errorf("re-encode payload: %w", err)
	}
	var out asc.AgeRatingDeclarationAttributes
	dec := json.NewDecoder(strings.NewReader(string(buf)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return asc.AgeRatingDeclarationAttributes{}, fmt.Errorf("decode: %w", err)
	}
	return out, nil
}

// knownAgeRatingKeys enumerates Apple's wire keys for the age-rating
// declaration. Sourced from the OpenAPI spec's
// AgeRatingDeclarationUpdateRequest. A typo in the user's YAML surfaces as
// "unknown key X" rather than silently dropping the value.
var knownAgeRatingKeys = map[string]struct{}{
	"advertising":                                 {},
	"ageAssurance":                                {},
	"ageRatingOverride":                           {},
	"ageRatingOverrideV2":                         {},
	"alcoholTobaccoOrDrugUseOrReferences":         {},
	"contests":                                    {},
	"developerAgeRatingInfoUrl":                   {},
	"gambling":                                    {},
	"gamblingSimulated":                           {},
	"gunsOrOtherWeapons":                          {},
	"healthOrWellnessTopics":                      {},
	"horrorOrFearThemes":                          {},
	"kidsAgeBand":                                 {},
	"koreaAgeRatingOverride":                      {},
	"lootBox":                                     {},
	"matureOrSuggestiveThemes":                    {},
	"medicalOrTreatmentInformation":               {},
	"messagingAndChat":                            {},
	"parentalControls":                            {},
	"profanityOrCrudeHumor":                       {},
	"sexualContentGraphicAndNudity":               {},
	"sexualContentOrNudity":                       {},
	"unrestrictedWebAccess":                       {},
	"userGeneratedContent":                        {},
	"violenceCartoonOrFantasy":                    {},
	"violenceRealistic":                           {},
	"violenceRealisticProlongedGraphicOrSadistic": {},
}

// assertKnownAgeRatingKeys returns a typed error naming any unknown keys in
// the user's payload. Catches typos before we hit Apple's API (which would
// silently ignore them).
func assertKnownAgeRatingKeys(raw map[string]any) error {
	var bad []string
	for k := range raw {
		if _, ok := knownAgeRatingKeys[k]; !ok {
			bad = append(bad, k)
		}
	}
	if len(bad) == 0 {
		return nil
	}
	sortStrings(bad)
	return fmt.Errorf("unknown age-rating keys: %s (see openapi.oas.json AgeRatingDeclarationUpdateRequest)", strings.Join(bad, ", "))
}

// validFrequencyEnum is the set of frequency-enum values Apple accepts on
// every frequency-shaped field.
var validFrequencyEnum = map[string]struct{}{
	"NONE":                {},
	"INFREQUENT_OR_MILD":  {},
	"FREQUENT_OR_INTENSE": {},
	"INFREQUENT":          {},
	"FREQUENT":            {},
}

// frequencyKeys are the attribute fields validated against validFrequencyEnum.
var frequencyKeys = []string{
	"alcoholTobaccoOrDrugUseOrReferences",
	"contests",
	"gamblingSimulated",
	"gunsOrOtherWeapons",
	"horrorOrFearThemes",
	"matureOrSuggestiveThemes",
	"medicalOrTreatmentInformation",
	"profanityOrCrudeHumor",
	"sexualContentGraphicAndNudity",
	"sexualContentOrNudity",
	"violenceCartoonOrFantasy",
	"violenceRealistic",
	"violenceRealisticProlongedGraphicOrSadistic",
}

// validateAgeRatingAttributes runs server-side gates locally so users see
// errors before a wasted API hit.
func validateAgeRatingAttributes(a asc.AgeRatingDeclarationAttributes) error {
	v := reflect.ValueOf(a)
	t := v.Type()
	for _, key := range frequencyKeys {
		field, ok := fieldByJSONTag(t, key)
		if !ok {
			continue
		}
		raw := v.FieldByIndex(field.Index).String()
		if raw == "" {
			continue
		}
		if _, ok := validFrequencyEnum[raw]; !ok {
			return fmt.Errorf("%s: %q is not a valid frequency enum (use NONE | INFREQUENT_OR_MILD | FREQUENT_OR_INTENSE | INFREQUENT | FREQUENT)", key, raw)
		}
	}
	return nil
}

// fieldByJSONTag finds the struct field whose `json:"<name>"` tag matches
// the requested wire name. Returns (zero, false) when not found.
func fieldByJSONTag(t reflect.Type, name string) (reflect.StructField, bool) {
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		if tag == "" {
			continue
		}
		if comma := strings.IndexByte(tag, ','); comma >= 0 {
			tag = tag[:comma]
		}
		if tag == name {
			return t.Field(i), true
		}
	}
	return reflect.StructField{}, false
}

// ----------------------------------------------------------------------------
// Diff — only fields the user supplied AND that differ from current
// ----------------------------------------------------------------------------

// diffAgeRating returns the subset of desired fields that differ from current.
// providedKeys gates the diff so we don't accidentally PATCH a field the user
// didn't set in their file (zero-value vs unset distinction).
func diffAgeRating(current, desired asc.AgeRatingDeclarationAttributes, providedKeys map[string]struct{}) map[string]any {
	out := map[string]any{}
	cv := reflect.ValueOf(current)
	dv := reflect.ValueOf(desired)
	t := cv.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("json")
		if tag == "" {
			continue
		}
		key := tag
		if comma := strings.IndexByte(tag, ','); comma >= 0 {
			key = tag[:comma]
		}
		if _, supplied := providedKeys[key]; !supplied {
			continue
		}
		c := cv.Field(i).Interface()
		d := dv.Field(i).Interface()
		if !reflect.DeepEqual(c, d) {
			out[key] = d
		}
	}
	return out
}

// ----------------------------------------------------------------------------
// API resolution helpers
// ----------------------------------------------------------------------------

// lookupVersionState resolves bundle+version+platform to the version's
// lifecycle state string. Used to pick the matching appInfo bucket.
func lookupVersionState(ctx context.Context, c *asc.Client, appID, versionStr, platform string) (string, error) {
	q := url.Values{
		"filter[versionString]": {versionStr},
		"limit":                 {"1"},
	}
	if platform != "" {
		q.Set("filter[platform]", platform)
	}
	page, err := asc.Get[asc.Collection[asc.VersionAttributes]](
		ctx, c, "/v1/apps/"+appID+"/appStoreVersions", q,
	)
	if err != nil {
		return "", err
	}
	if len(page.Data) == 0 {
		return "", fmt.Errorf("age-rating set: no version %q found (platform=%s)", versionStr, platform)
	}
	return versionDisplayState(page.Data[0].Attributes), nil
}

// fetchAgeRatingDeclaration returns the current questionnaire attributes plus
// the declaration ID for PATCHing. The declaration is a singleton on the
// appInfo — Apple returns Single[…], not a collection.
func fetchAgeRatingDeclaration(ctx context.Context, c *asc.Client, appInfoID string) (asc.AgeRatingDeclarationAttributes, string, error) {
	resp, err := asc.Get[asc.Single[asc.AgeRatingDeclarationAttributes]](
		ctx, c, "/v1/appInfos/"+appInfoID+"/ageRatingDeclaration", nil,
	)
	if err != nil {
		return asc.AgeRatingDeclarationAttributes{}, "", err
	}
	if resp.Data.ID == "" {
		return asc.AgeRatingDeclarationAttributes{}, "", fmt.Errorf("age-rating set: appInfo %q has no ageRatingDeclaration", appInfoID)
	}
	return resp.Data.Attributes, resp.Data.ID, nil
}

// sortStrings sorts a string slice in place. Tiny wrapper to avoid an
// import-only-for-sort.Strings in this file.
func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j-1] > ss[j]; j-- {
			ss[j-1], ss[j] = ss[j], ss[j-1]
		}
	}
}
