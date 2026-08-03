package cli

import (
	"fmt"
	"strings"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/store"
	"github.com/spf13/cobra"
)

var decisionsCmd = &cobra.Command{
	Use:   "decisions",
	Short: "Review and approve proposed access decisions",
}

var decisionsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List locally recorded decisions",
	RunE:  runDecisionsList,
}

var decisionsApproveCmd = &cobra.Command{
	Use:   "approve <decision-id>",
	Short: "Approve a proposed decision for later execution",
	Args:  cobra.ExactArgs(1),
	RunE:  runDecisionsApprove,
}

var decisionsRejectCmd = &cobra.Command{
	Use:   "reject <decision-id>",
	Short: "Reject a proposed or approved decision with a reusable reason",
	Args:  cobra.ExactArgs(1),
	RunE:  runDecisionsReject,
}

var decisionsAttestOwnerCmd = &cobra.Command{
	Use:   "attest-owner <decision-id>",
	Short: "Record a named owner for a non-human identity decision",
	Args:  cobra.ExactArgs(1),
	RunE:  runDecisionsAttestOwner,
}

var (
	decisionsProvider string
	decisionsSubject  string
	decisionsStatus   string
	decisionsLimit    int
	decisionActor     string
	rejectReason      string
	attestedOwner     string
	attestReason      string
)

func init() {
	decisionsListCmd.Flags().StringVar(&decisionsProvider, "provider", "", "Filter by provider")
	decisionsListCmd.Flags().StringVar(&decisionsSubject, "subject", "", "Filter by subject")
	decisionsListCmd.Flags().StringVar(&decisionsStatus, "status", "", "Filter by decision status")
	decisionsListCmd.Flags().IntVar(&decisionsLimit, "limit", 100, "Maximum decisions to show")

	decisionsApproveCmd.Flags().StringVar(&decisionActor, "by", "cli", "Approver identity recorded in the ledger")
	decisionsRejectCmd.Flags().StringVar(&decisionActor, "by", "cli", "Rejector identity recorded in the ledger")
	decisionsRejectCmd.Flags().StringVar(&rejectReason, "reason", "", "Reason to store with the rejection")
	decisionsAttestOwnerCmd.Flags().StringVar(&decisionActor, "by", "cli", "Actor identity recorded in the ledger")
	decisionsAttestOwnerCmd.Flags().StringVar(&attestedOwner, "owner", "", "Owner identity to attach to the non-human identity")
	decisionsAttestOwnerCmd.Flags().StringVar(&attestReason, "reason", "", "Reason or context for the attestation")

	decisionsCmd.AddCommand(decisionsListCmd, decisionsApproveCmd, decisionsRejectCmd, decisionsAttestOwnerCmd)
	rootCmd.AddCommand(decisionsCmd)
}

func runDecisionsList(cmd *cobra.Command, _ []string) error {
	db, err := openStore()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	filter, err := decisionFilterFromFlags()
	if err != nil {
		return err
	}
	decisions, err := db.ListDecisions(cmd.Context(), filter)
	if err != nil {
		return err
	}

	if jsonOutput {
		return printJSON(decisions)
	}
	if len(decisions) == 0 {
		fmt.Println("No decisions recorded yet. Run `unseat offboard <email>` to create proposed decisions.")
		return nil
	}

	rows := make([][]string, 0, len(decisions))
	for _, d := range decisions {
		rows = append(rows, []string{
			d.ID,
			d.Subject,
			d.Provider,
			string(d.ActionClass),
			string(d.Risk),
			string(d.Status),
			compactReason(d.Reason),
		})
	}
	printTable([]string{"ID", "SUBJECT", "PROVIDER", "ACTION", "RISK", "STATUS", "WHY"}, rows)
	return nil
}

func runDecisionsApprove(cmd *cobra.Command, args []string) error {
	db, err := openStore()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	decision, err := db.ApproveDecision(cmd.Context(), args[0], decisionActor)
	if err != nil {
		return err
	}
	if jsonOutput {
		return printJSON(decision)
	}
	fmt.Printf("Approved %s by %s.\n", decision.ID, decision.ApprovedBy)
	return nil
}

func runDecisionsReject(cmd *cobra.Command, args []string) error {
	db, err := openStore()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	decision, err := db.RejectDecision(cmd.Context(), args[0], decisionActor, rejectReason)
	if err != nil {
		return err
	}
	if jsonOutput {
		return printJSON(decision)
	}
	fmt.Printf("Rejected %s by %s: %s\n", decision.ID, decision.RejectedBy, decision.RejectedReason)
	return nil
}

func runDecisionsAttestOwner(cmd *cobra.Command, args []string) error {
	db, err := openStore()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	decision, err := db.AttestOwner(cmd.Context(), args[0], attestedOwner, decisionActor, attestReason)
	if err != nil {
		return err
	}
	if jsonOutput {
		return printJSON(decision)
	}
	fmt.Printf("Attested owner %s for %s by %s.\n", decision.Metadata["attested_owner"], decision.ID, decision.Metadata["attested_by"])
	return nil
}

func decisionFilterFromFlags() (store.DecisionFilter, error) {
	filter := store.DecisionFilter{Limit: decisionsLimit}
	if decisionsProvider != "" {
		provider := strings.ToLower(strings.TrimSpace(decisionsProvider))
		filter.Provider = &provider
	}
	if decisionsSubject != "" {
		subject := strings.ToLower(strings.TrimSpace(decisionsSubject))
		filter.Subject = &subject
	}
	if decisionsStatus != "" {
		status := core.DecisionStatus(strings.TrimSpace(decisionsStatus))
		switch status {
		case core.DecisionProposed,
			core.DecisionApproved,
			core.DecisionRejected,
			core.DecisionExecuted,
			core.DecisionVerified,
			core.DecisionBlocked,
			core.DecisionStale,
			core.DecisionVerificationFailed:
			filter.Status = &status
		default:
			return store.DecisionFilter{}, fmt.Errorf("unknown decision status %q", decisionsStatus)
		}
	}
	return filter, nil
}
