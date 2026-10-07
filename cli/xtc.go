package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/VladGavrila/matrixreq-cli/internal/api"
	"github.com/VladGavrila/matrixreq-cli/internal/execution"
	"github.com/VladGavrila/matrixreq-cli/internal/fieldmap"
	"github.com/VladGavrila/matrixreq-cli/internal/output"
	"github.com/spf13/cobra"
)

var xtcCmd = &cobra.Command{
	Use:   "xtc",
	Short: "Executed Test Case (XTC) operations",
}

var xtcUploadCmd = &cobra.Command{
	Use:   "upload <results.yaml>",
	Short: "Upload execution results to XTCs",
	Long: `Parse a YAML results file and upload execution results to the corresponding
XTCs in the specified folder. The YAML file should contain test results with
step-level pass/fail status matched by requirement links.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		project, err := requireProject()
		if err != nil {
			return err
		}

		folder, _ := cmd.Flags().GetString("folder")

		fm, err := fieldmap.LoadOrFetch(svc, project)
		if err != nil {
			return fmt.Errorf("loading field map: %w", err)
		}

		results, err := execution.ParseYAMLResults(args[0])
		if err != nil {
			return fmt.Errorf("parsing results: %w", err)
		}

		uploadResult, err := execution.UploadResults(svc, project, folder, results, fm)
		if err != nil {
			return err
		}

		strict, _ := cmd.Flags().GetBool("strict")

		if getOutputFormat() == "json" {
			if err := output.PrintItem(getOutputFormat(), uploadResult); err != nil {
				return err
			}
		} else {
			printUploadSummary(uploadResult)
		}
		return strictUploadError(uploadResult, strict)
	},
}

// strictUploadError turns refused XTCs into a failure when --strict is set, so
// CI and scripts notice a skipped XTC from the exit code alone. The summary or
// JSON has already been printed by the time this runs. Without --strict an
// out-of-sync XTC is reported but does not fail the command.
func strictUploadError(uploadResult *execution.UploadResult, strict bool) error {
	if !strict || len(uploadResult.Skipped) == 0 {
		return nil
	}
	return fmt.Errorf("%d XTC(s) out of sync and left untouched: %s",
		len(uploadResult.Skipped), strings.Join(sortedKeys(uploadResult.Skipped), ", "))
}

// sortedKeys returns a map's keys in order, so output is stable run to run.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// printUploadSummary reports what an upload wrote, what it refused, and why.
// A refused XTC is called out separately from a failed one: nothing was sent,
// so Matrix still holds whatever it held before.
//
// Every refusal already has an operator-facing message in Issues, naming the
// XTC and what drifted, so it is not repeated as its own block; the count at the
// end is what marks the XTCs as refused. Refs are sorted for stable output.
func printUploadSummary(uploadResult *execution.UploadResult) {
	successCount := 0
	for _, ref := range sortedKeys(uploadResult.Successes) {
		if uploadResult.Successes[ref] {
			successCount++
			fmt.Printf("  Updated %s\n", ref)
		}
	}

	if len(uploadResult.Issues) > 0 {
		fmt.Println("Issues:")
		for _, issue := range uploadResult.Issues {
			fmt.Printf("  %s\n", issue)
		}
	}

	fmt.Printf("\nUploaded %d/%d XTCs successfully", successCount, len(uploadResult.Successes))
	if len(uploadResult.Skipped) > 0 {
		fmt.Printf(" (%d out of sync, left untouched, manual review needed: %s)",
			len(uploadResult.Skipped), strings.Join(sortedKeys(uploadResult.Skipped), ", "))
	}
	fmt.Println()
}

var xtcStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Compute execution statistics for an XTC folder",
	Long: `Fetch all XTCs in the specified folder and compute execution statistics
including per-test status, step counts, requirement coverage, and aggregated totals.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		project, err := requireProject()
		if err != nil {
			return err
		}

		folder, _ := cmd.Flags().GetString("folder")

		fm, err := fieldmap.LoadOrFetch(svc, project)
		if err != nil {
			return fmt.Errorf("loading field map: %w", err)
		}

		stats, err := execution.ComputeStats(svc, project, folder, fm)
		if err != nil {
			return err
		}

		if getOutputFormat() == "json" {
			return output.PrintItem(getOutputFormat(), stats.ToDict())
		}

		// Print summary table
		fmt.Println("XTC Execution Statistics")
		fmt.Println(strings.Repeat("-", 60))

		// Per-test stats
		if len(stats.XTCStats) > 0 {
			headers := []string{"XTC", "Status", "Steps", "Passed", "Failed", "Not Exec"}
			var rows [][]string
			for ref, s := range stats.XTCStats {
				rows = append(rows, []string{
					ref,
					s.TestStatus,
					fmt.Sprintf("%d", s.NumSteps),
					fmt.Sprintf("%d", s.NumPassed),
					fmt.Sprintf("%d", s.NumFailed),
					fmt.Sprintf("%d", s.NumNotExecutedWithReq),
				})
			}
			if err := output.Print(getOutputFormat(), headers, rows); err != nil {
				return err
			}
		}

		// Totals
		fmt.Printf("\nTotals:\n")
		fmt.Printf("  Tests executed:     %d\n", stats.TotalTestsExecuted)
		fmt.Printf("  Tests in progress:  %d\n", stats.TotalTestsInProgress)
		fmt.Printf("  Tests not executed: %d\n", stats.TotalTestsNotExecuted)
		fmt.Printf("  Total steps:        %d\n", stats.TotalSteps)
		fmt.Printf("  Steps passed:       %d\n", stats.TotalPassed)
		fmt.Printf("  Steps failed:       %d\n", stats.TotalFailed)

		// SOFT coverage summary
		if len(stats.OverallSOFTCoverage) > 0 {
			fmt.Printf("\nRequirement Coverage:\n")
			for _, result := range []string{"passed", "failed", "pass with issue", "Not Executed"} {
				count := stats.ExecutedSOFTTotals[result]
				if count > 0 {
					fmt.Printf("  %s: %d\n", result, count)
				}
			}
		}

		return nil
	},
}

var xtcCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create XTC run from a TC folder(s)",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		project, err := requireProject()
		if err != nil {
			return err
		}

		inputs, _ := cmd.Flags().GetStringArray("input")
		parent, _ := cmd.Flags().GetString("parent")
		reason, _ := cmd.Flags().GetString("reason")
		maps, _ := cmd.Flags().GetStringArray("map")
		presets, _ := cmd.Flags().GetStringArray("preset")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		fm, err := fieldmap.LoadOrFetch(svc, project)
		if err != nil {
			return fmt.Errorf("loading field map: %w", err)
		}

		req := &api.ExecuteRequest{
			Input:        inputs,
			Output:       "XTC",
			ParentFolder: parent,
			Reason:       reason,
		}

		for _, m := range maps {
			parts := strings.SplitN(m, "=", 2)
			if len(parts) != 2 {
				return fmt.Errorf("--map %q: expected Category.Label=Category.Label", m)
			}
			fromID, err := resolveFieldLabel(fm, parts[0])
			if err != nil {
				return fmt.Errorf("--map from %q: %w", parts[0], err)
			}
			toID, err := resolveFieldLabel(fm, parts[1])
			if err != nil {
				return fmt.Errorf("--map to %q: %w", parts[1], err)
			}
			req.ItemFieldMapping = append(req.ItemFieldMapping, api.ExecuteFieldMap{FromID: fromID, ToID: toID})
		}

		for _, p := range presets {
			parts := strings.SplitN(p, "=", 2)
			if len(parts) != 2 {
				return fmt.Errorf("--preset %q: expected Category.Label=Value", p)
			}
			fieldID, err := resolveFieldLabel(fm, parts[0])
			if err != nil {
				return fmt.Errorf("--preset %q: %w", parts[0], err)
			}
			req.ItemPresets = append(req.ItemPresets, api.ExecutePreset{Field: fieldID, Value: parts[1]})
		}

		if dryRun {
			return output.PrintItem("json", req)
		}

		resp, err := svc.Items.Execute(project, req)
		if err != nil {
			return err
		}

		if getOutputFormat() == "json" {
			return output.PrintItem(getOutputFormat(), resp)
		}
		fmt.Printf("Run folder: %s\n", resp.Folder)
		for _, e := range resp.XTCInError {
			fmt.Printf("Error %s: %s\n", e.Key, strings.Join(e.Errors, "; "))
		}
		return nil
	},
}

var xtcExecuteCmd = &cobra.Command{
	Use:   "execute",
	Short: "Execute TC folders into an XTC run and overlay results",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		project, err := requireProject()
		if err != nil {
			return err
		}

		folder, _ := cmd.Flags().GetString("folder")
		inputs, _ := cmd.Flags().GetStringArray("input")
		parent, _ := cmd.Flags().GetString("parent")
		resultsFile, _ := cmd.Flags().GetString("results")
		resultsDir, _ := cmd.Flags().GetString("results-dir")
		reason, _ := cmd.Flags().GetString("reason")
		maps, _ := cmd.Flags().GetStringArray("map")
		presets, _ := cmd.Flags().GetStringArray("preset")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		if resultsFile == "" && resultsDir == "" {
			return fmt.Errorf("one of --results or --results-dir is required")
		}
		if resultsFile == "" {
			resultsFile, err = latestResultsFile(resultsDir)
			if err != nil {
				return err
			}
		}

		results, err := execution.ParseYAMLResults(resultsFile)
		if err != nil {
			return fmt.Errorf("parsing results: %w", err)
		}

		fm, err := fieldmap.LoadOrFetch(svc, project)
		if err != nil {
			return fmt.Errorf("loading field map: %w", err)
		}

		if folder == "" {
			req := &api.ExecuteRequest{
				Input:        inputs,
				Output:       "XTC",
				ParentFolder: parent,
				Reason:       reason,
			}
			for _, m := range maps {
				parts := strings.SplitN(m, "=", 2)
				if len(parts) != 2 {
					return fmt.Errorf("--map %q: expected Category.Label=Category.Label", m)
				}
				fromID, err := resolveFieldLabel(fm, parts[0])
				if err != nil {
					return fmt.Errorf("--map from %q: %w", parts[0], err)
				}
				toID, err := resolveFieldLabel(fm, parts[1])
				if err != nil {
					return fmt.Errorf("--map to %q: %w", parts[1], err)
				}
				req.ItemFieldMapping = append(req.ItemFieldMapping, api.ExecuteFieldMap{FromID: fromID, ToID: toID})
			}
			for _, p := range presets {
				parts := strings.SplitN(p, "=", 2)
				if len(parts) != 2 {
					return fmt.Errorf("--preset %q: expected Category.Label=Value", p)
				}
				fieldID, err := resolveFieldLabel(fm, parts[0])
				if err != nil {
					return fmt.Errorf("--preset %q: %w", parts[0], err)
				}
				req.ItemPresets = append(req.ItemPresets, api.ExecutePreset{Field: fieldID, Value: parts[1]})
			}

			if dryRun {
				return output.PrintItem("json", req)
			}

			resp, err := svc.Items.Execute(project, req)
			if err != nil {
				return err
			}
			folder = resp.Folder
			fmt.Printf("Run folder: %s\n", folder)
		}

		if dryRun {
			fmt.Println("dry-run: skipping upload step")
			return nil
		}

		uploadResult, err := execution.UploadResults(svc, project, folder, results, fm)
		if err != nil {
			return fmt.Errorf("uploading results: %w", err)
		}

		strict, _ := cmd.Flags().GetBool("strict")
		printUploadSummary(uploadResult)
		return strictUploadError(uploadResult, strict)
	},
}

// resolveFieldLabel parses "Category.FieldLabel" and returns the numeric field ID.
func resolveFieldLabel(fm *fieldmap.FieldMap, spec string) (int, error) {
	parts := strings.SplitN(spec, ".", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("expected Category.FieldLabel, got %q", spec)
	}
	return fm.Resolve(parts[0], parts[1])
}

// latestResultsFile returns the path of the most recently modified results_*.yaml in dir.
func latestResultsFile(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("reading results dir %q: %w", dir, err)
	}
	var bestModTime int64
	var bestPath string
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "results_") || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if t := info.ModTime().UnixNano(); t > bestModTime {
			bestModTime = t
			bestPath = filepath.Join(dir, e.Name())
		}
	}
	if bestPath == "" {
		return "", fmt.Errorf("no results_*.yaml files found in %q", dir)
	}
	return bestPath, nil
}

func init() {
	rootCmd.AddCommand(xtcCmd)
	xtcCmd.AddCommand(xtcUploadCmd)
	xtcCmd.AddCommand(xtcStatsCmd)
	xtcCmd.AddCommand(xtcCreateCmd)
	xtcCmd.AddCommand(xtcExecuteCmd)

	xtcUploadCmd.Flags().StringP("folder", "f", "", "Target XTC folder (e.g., F-XTC-123)")
	_ = xtcUploadCmd.MarkFlagRequired("folder")
	xtcUploadCmd.Flags().Bool("strict", false, "Exit non-zero when any XTC is out of sync and left untouched")

	xtcStatsCmd.Flags().StringP("folder", "f", "", "Target XTC folder (e.g., F-XTC-123)")
	_ = xtcStatsCmd.MarkFlagRequired("folder")

	xtcCreateCmd.Flags().StringArray("input", nil, "TC folder refs (repeatable)")
	xtcCreateCmd.Flags().String("parent", "", "Target XTC parent folder ref")
	xtcCreateCmd.Flags().StringP("reason", "r", "", "Reason for execution")
	xtcCreateCmd.Flags().StringArray("map", nil, "Field mapping: Category.Label=Category.Label (repeatable)")
	xtcCreateCmd.Flags().StringArray("preset", nil, "XTC field preset: Category.Label=Value (repeatable)")
	xtcCreateCmd.Flags().Bool("dry-run", false, "Print resolved ExecuteRequest as JSON and exit")
	_ = xtcCreateCmd.MarkFlagRequired("reason")
	_ = xtcCreateCmd.MarkFlagRequired("input")
	_ = xtcCreateCmd.MarkFlagRequired("parent")

	xtcExecuteCmd.Flags().String("results", "", "Path to results YAML file")
	xtcExecuteCmd.Flags().String("results-dir", "", "Dir to scan for latest results_*.yaml")
	xtcExecuteCmd.Flags().StringArray("input", nil, "TC folder refs (used when --folder not given)")
	xtcExecuteCmd.Flags().String("parent", "", "Target XTC parent folder ref (used when --folder not given)")
	xtcExecuteCmd.Flags().StringP("folder", "f", "", "Reuse existing XTC run folder (skips create step)")
	xtcExecuteCmd.Flags().StringP("reason", "r", "", "Reason")
	xtcExecuteCmd.Flags().StringArray("map", nil, "Field mapping: Category.Label=Category.Label (repeatable)")
	xtcExecuteCmd.Flags().StringArray("preset", nil, "XTC field preset: Category.Label=Value (repeatable)")
	xtcExecuteCmd.Flags().Bool("strict", false, "Exit non-zero when any XTC is out of sync and left untouched")
	xtcExecuteCmd.Flags().Bool("dry-run", false, "Print resolved ExecuteRequest as JSON and exit (skips upload)")
	_ = xtcExecuteCmd.MarkFlagRequired("reason")
	xtcExecuteCmd.MarkFlagsMutuallyExclusive("folder", "input")
	xtcExecuteCmd.MarkFlagsMutuallyExclusive("folder", "parent")
	xtcExecuteCmd.MarkFlagsMutuallyExclusive("results", "results-dir")
}
