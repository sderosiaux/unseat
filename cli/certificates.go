package cli

import (
	"fmt"
	"strings"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/store"
	"github.com/spf13/cobra"
)

var certificatesCmd = &cobra.Command{
	Use:   "certificates",
	Short: "Inspect stored offboarding certificates",
}

var certificatesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List stored offboarding certificates",
	RunE:  runCertificatesList,
}

var certificatesShowCmd = &cobra.Command{
	Use:   "show <certificate-id>",
	Short: "Show one stored offboarding certificate",
	Args:  cobra.ExactArgs(1),
	RunE:  runCertificatesShow,
}

var (
	certificateSubject string
	certificateStatus  string
	certificateLimit   int
)

func init() {
	certificatesListCmd.Flags().StringVar(&certificateSubject, "subject", "", "Filter by subject")
	certificatesListCmd.Flags().StringVar(&certificateStatus, "status", "", "Filter by certificate status")
	certificatesListCmd.Flags().IntVar(&certificateLimit, "limit", 50, "Maximum certificates to show")
	certificatesCmd.AddCommand(certificatesListCmd, certificatesShowCmd)
	rootCmd.AddCommand(certificatesCmd)
}

func runCertificatesList(cmd *cobra.Command, _ []string) error {
	db, err := openStore()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	filter, err := certificateFilterFromFlags()
	if err != nil {
		return err
	}
	certs, err := db.ListOffboardingCertificates(cmd.Context(), filter)
	if err != nil {
		return err
	}
	if jsonOutput {
		return printJSON(certs)
	}
	if len(certs) == 0 {
		fmt.Println("No offboarding certificates recorded yet. Run `unseat offboard <email>`.")
		return nil
	}
	rows := make([][]string, 0, len(certs))
	for _, cert := range certs {
		rows = append(rows, []string{
			cert.ID,
			cert.Subject.PrimaryEmail,
			string(cert.Status),
			fmt.Sprintf("%d", len(cert.Providers)),
			fmt.Sprintf("%d", len(cert.Decisions)),
			fmt.Sprintf("%d", len(cert.Unknowns)),
			cert.StartedAt.Format("2006-01-02 15:04:05"),
		})
	}
	printTable([]string{"ID", "SUBJECT", "STATUS", "PROVIDERS", "DECISIONS", "UNKNOWNS", "STARTED"}, rows)
	return nil
}

func runCertificatesShow(cmd *cobra.Command, args []string) error {
	db, err := openStore()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	cert, err := db.GetOffboardingCertificate(cmd.Context(), args[0])
	if err != nil {
		return err
	}
	if cert == nil {
		return fmt.Errorf("certificate %q not found", args[0])
	}
	if jsonOutput {
		return printJSON(cert)
	}
	printOffboardingCertificate(cert, "(stored in local ledger)")
	return nil
}

func certificateFilterFromFlags() (store.CertificateFilter, error) {
	filter := store.CertificateFilter{Limit: certificateLimit}
	if certificateSubject != "" {
		subject := strings.ToLower(strings.TrimSpace(certificateSubject))
		filter.Subject = &subject
	}
	if certificateStatus != "" {
		status := core.CertificateStatus(strings.TrimSpace(certificateStatus))
		switch status {
		case core.CertificateComplete,
			core.CertificateCompleteWithProviderLimits,
			core.CertificateBlocked,
			core.CertificateIncomplete,
			core.CertificateStale:
			filter.Status = &status
		default:
			return store.CertificateFilter{}, fmt.Errorf("unknown certificate status %q", certificateStatus)
		}
	}
	return filter, nil
}
