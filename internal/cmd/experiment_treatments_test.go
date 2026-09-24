package cmd

import "testing"

func TestF8C_TreatmentAndLocalizationWritesRequireConfirm(t *testing.T) {
	root := newExperimentsCommand()
	for _, path := range [][]string{{"treatments", "create"}, {"treatments", "update"}, {"treatments", "delete"}, {"treatments", "localizations", "create"}, {"treatments", "localizations", "delete"}, {"treatments", "localizations", "assets", "upload"}, {"treatments", "localizations", "assets", "delete"}} {
		cmd, _, err := root.Find(path)
		if err != nil || cmd.Flags().Lookup("confirm") == nil {
			t.Fatalf("%v confirm missing: %v", path, err)
		}
	}
}
