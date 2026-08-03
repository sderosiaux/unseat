package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/offboarding"
	"github.com/sderosiaux/unseat/internal/provider"
	"github.com/spf13/cobra"
)

var offboardCmd = &cobra.Command{
	Use:   "offboard <email-or-identifier>",
	Short: "Produce an actionable offboarding certificate without mutating providers",
	Long: `Read providers, billing APIs, non-human access inventories and the
identity source, then produce the decisions and evidence needed to finish one
offboarding.

This command observes only. It never calls provider write APIs.`,
	Args: cobra.ExactArgs(1),
	RunE: runOffboard,
}

var (
	offboardProviders   []string
	offboardEvidenceDir string
)

func init() {
	offboardCmd.Flags().StringSliceVar(&offboardProviders, "provider", nil, "Restrict the certificate to these providers (default: all configured)")
	offboardCmd.Flags().StringVar(&offboardEvidenceDir, "evidence-dir", ".unseat/evidence", "Directory for local offboarding certificate artifacts")
	rootCmd.AddCommand(offboardCmd)
}

type offboardOutput struct {
	Certificate  *core.OffboardingCertificate `json:"certificate"`
	ArtifactPath string                       `json:"artifact_path,omitempty"`
}

func runOffboard(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if err := requireIdentitySource(cfg); err != nil {
		return err
	}

	reg, identity, err := provider.BuildRegistry(cmd.Context(), cfg)
	if err != nil {
		return fmt.Errorf("initialize providers: %w", err)
	}
	if len(offboardProviders) == 0 && len(configuredProviders(cfg)) == 0 {
		return fmt.Errorf("no configured SaaS provider credentials found")
	}

	cert, err := offboarding.RunObserve(cmd.Context(), offboarding.Input{
		Config:    cfg,
		Registry:  reg,
		Identity:  identity,
		Subject:   args[0],
		Providers: offboardProviders,
		Actor:     "cli",
	})
	if err != nil {
		return err
	}

	path, err := writeOffboardingCertificate(cert, offboardEvidenceDir)
	if err != nil {
		return err
	}

	if jsonOutput {
		return printJSON(offboardOutput{Certificate: cert, ArtifactPath: path})
	}

	printOffboardingCertificate(cert, path)
	return nil
}

func writeOffboardingCertificate(cert *core.OffboardingCertificate, dir string) (string, error) {
	if dir == "" {
		dir = ".unseat/evidence"
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create evidence directory: %w", err)
	}
	data, err := json.MarshalIndent(cert, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode offboarding certificate: %w", err)
	}
	name := fmt.Sprintf("%s-%s.json", cert.StartedAt.Format("20060102T150405Z"), safeArtifactName(cert.Subject.PrimaryEmail))
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return "", fmt.Errorf("write offboarding certificate: %w", err)
	}
	return path, nil
}

func safeArtifactName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '@' || r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

func printOffboardingCertificate(cert *core.OffboardingCertificate, artifactPath string) {
	fmt.Printf("OFFBOARDING CERTIFICATE  %s\n", cert.ID)
	fmt.Printf("Subject: %s", cert.Subject.PrimaryEmail)
	if cert.Subject.DirectoryStatus != "" {
		fmt.Printf(" [%s]", cert.Subject.DirectoryStatus)
	}
	fmt.Printf("\nStatus: %s\n", cert.Status)
	fmt.Printf("Artifact: %s\n", artifactPath)

	rows := make([][]string, 0, len(cert.Providers))
	for _, p := range cert.Providers {
		rows = append(rows, []string{
			p.Provider,
			fmt.Sprintf("%d", len(p.Seats)),
			fmt.Sprintf("%d", len(p.Credentials)),
			fmt.Sprintf("%d", len(p.NonHumanIdentities)),
			claimSummary(p.BillingClaims),
			fmt.Sprintf("%d", len(p.Unknowns)),
			fmt.Sprintf("%d", len(p.Errors)),
		})
	}
	if len(rows) > 0 {
		fmt.Println()
		printTable([]string{"PROVIDER", "SEATS", "CREDS", "NHI", "BILLING", "UNKNOWNS", "ERRORS"}, rows)
	}

	if len(cert.Decisions) == 0 {
		fmt.Println("\nNo decisions for this subject.")
	} else {
		fmt.Printf("\nDECISIONS (%d)\n", len(cert.Decisions))
		decisionRows := make([][]string, 0, len(cert.Decisions))
		for _, d := range cert.Decisions {
			decisionRows = append(decisionRows, []string{
				d.Provider,
				string(d.ActionClass),
				string(d.ObjectKind),
				string(d.Risk),
				string(d.Status),
				compactReason(d.Reason),
			})
		}
		printTable([]string{"PROVIDER", "ACTION", "OBJECT", "RISK", "STATUS", "WHY"}, decisionRows)
	}

	if len(cert.Unknowns) > 0 {
		fmt.Printf("\nUNKNOWN / PROVIDER LIMITS (%d)\n", len(cert.Unknowns))
		for _, unknown := range cert.Unknowns {
			fmt.Printf("  - %s\n", unknown)
		}
	}
}

func claimSummary(claims []core.BillingClaim) string {
	if len(claims) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(claims))
	for _, claim := range claims {
		label := string(claim.Type)
		if claim.AmountMinor != nil {
			label += " " + formatMoneyMinor(*claim.AmountMinor, claim.Currency)
		}
		parts = append(parts, label)
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func compactReason(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const max = 96
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
