package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/upmio/kubespray-upm/upmctl/internal/admission"
	"github.com/upmio/kubespray-upm/upmctl/internal/approval"
	upmdigest "github.com/upmio/kubespray-upm/upmctl/internal/digest"
	"github.com/upmio/kubespray-upm/upmctl/internal/readiness"
)

func TestApprovalApplicationLifecycleRemainsNonExecutable(t *testing.T) {
	workspace, kubeconfig := managedPlanWorkspace(t)
	createdAt := time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)
	created, appErr := New(newPlanFixtureRunner(kubeconfig)).PlanVMStart(context.Background(), workspace, workspace, "k8s-3", createdAt)
	if appErr != nil {
		t.Fatalf("PlanVMStart() error = %#v", appErr)
	}
	service := New(newPlanFixtureRunner(kubeconfig))
	beforePrepare := snapshotTree(t, workspace)
	prepared, appErr := service.PrepareApproval(context.Background(), workspace, workspace, created.PlanID, fixedClock(createdAt.Add(time.Minute)))
	if appErr != nil {
		t.Fatalf("PrepareApproval() error = %#v", appErr)
	}
	if prepared.Kind != "ApprovalPreparation" || prepared.Plan.PlanID != created.PlanID ||
		prepared.Preflight.PreflightStatus != readiness.PreflightPassed ||
		prepared.Preflight.ApprovalStatus != readiness.ApprovalMissing || prepared.ExecutionAvailable {
		t.Fatalf("preparation = %#v", prepared)
	}
	if afterPrepare := snapshotTree(t, workspace); !reflect.DeepEqual(afterPrepare, beforePrepare) {
		t.Fatalf("PrepareApproval changed workspace\nbefore: %#v\nafter:  %#v", beforePrepare, afterPrepare)
	}

	grantAt := createdAt.Add(2 * time.Minute)
	evidence := approvalEvidence(t, "planned worker recovery")
	grantRunner := newPlanFixtureRunner(kubeconfig)
	granted, appErr := New(grantRunner).GrantApproval(context.Background(), workspace, workspace, created.PlanID, evidence, fixedClock(grantAt))
	if appErr != nil {
		t.Fatalf("GrantApproval() error = %#v", appErr)
	}
	if granted.PlanID != created.PlanID || granted.ApprovedAt != grantAt.Format(time.RFC3339Nano) ||
		granted.HumanPresence.ChallengeDigest != evidence.ChallengeDigest || granted.HumanPresence.Terminal != evidence.Terminal ||
		granted.Approver.UID == "" || granted.Approver.Username == "" || granted.Approver.Hostname == "" {
		t.Fatalf("granted Approval = %#v", granted)
	}
	assertReadOnlyCommands(t, grantRunner.commands)
	approvalPath := filepath.Join(workspace, ".upmctl", "approvals", "by-plan", created.PlanID+".json")
	approvalContents, err := os.ReadFile(approvalPath)
	if err != nil {
		t.Fatalf("read Approval: %v", err)
	}
	if strings.Contains(string(approvalContents), "CONFIRM-TEST") {
		t.Fatal("stored Approval contains raw terminal challenge")
	}

	inspection, appErr := New(&countingRunner{}).GetApproval(workspace, workspace, granted.ApprovalID, grantAt.Add(time.Minute))
	if appErr != nil {
		t.Fatalf("GetApproval() error = %#v", appErr)
	}
	if inspection.Kind != "ApprovalInspection" || inspection.Status != readiness.ApprovalApproved ||
		inspection.Revocation != nil || inspection.ExecutionAvailable {
		t.Fatalf("approved inspection = %#v", inspection)
	}
	items, appErr := New(&countingRunner{}).ListApprovals(workspace, workspace, created.PlanID, grantAt.Add(time.Minute))
	if appErr != nil || len(items) != 1 || items[0].Approval.ApprovalID != granted.ApprovalID {
		t.Fatalf("ListApprovals() = %#v, %#v", items, appErr)
	}

	approvedPreflight, appErr := New(newPlanFixtureRunner(kubeconfig)).PreflightPlan(context.Background(), workspace, workspace, created.PlanID, fixedClock(grantAt.Add(time.Minute)))
	if appErr != nil {
		t.Fatalf("approved PreflightPlan() error = %#v", appErr)
	}
	if approvedPreflight.PreflightStatus != readiness.PreflightPassed || approvedPreflight.ApprovalStatus != readiness.ApprovalApproved ||
		approvedPreflight.ApplyDecision != readiness.ApplyDecisionBlocked || approvedPreflight.ExecutionAvailable {
		t.Fatalf("approved preflight = %#v", approvedPreflight)
	}

	revokedAt := grantAt.Add(2 * time.Minute)
	revocation, appErr := New(&countingRunner{}).RevokeApproval(workspace, workspace, granted.ApprovalID, approvalEvidence(t, "approval no longer intended"), fixedClock(revokedAt))
	if appErr != nil {
		t.Fatalf("RevokeApproval() error = %#v", appErr)
	}
	if revocation.ApprovalID != granted.ApprovalID || revocation.RevokedAt != revokedAt.Format(time.RFC3339Nano) {
		t.Fatalf("revocation = %#v", revocation)
	}
	afterApprovalContents, err := os.ReadFile(approvalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterApprovalContents) != string(approvalContents) {
		t.Fatal("RevokeApproval modified immutable Approval")
	}

	revokedInspection, appErr := New(&countingRunner{}).GetApproval(workspace, workspace, granted.ApprovalID, revokedAt.Add(time.Minute))
	if appErr != nil {
		t.Fatalf("GetApproval(revoked) error = %#v", appErr)
	}
	if revokedInspection.Status != readiness.ApprovalRevoked || revokedInspection.Revocation == nil ||
		revokedInspection.Revocation.RevocationID != revocation.RevocationID {
		t.Fatalf("revoked inspection = %#v", revokedInspection)
	}
	revokedPreflight, appErr := New(newPlanFixtureRunner(kubeconfig)).PreflightPlan(context.Background(), workspace, workspace, created.PlanID, fixedClock(revokedAt.Add(time.Minute)))
	if appErr != nil {
		t.Fatalf("revoked PreflightPlan() error = %#v", appErr)
	}
	if revokedPreflight.PreflightStatus != readiness.PreflightPassed || revokedPreflight.ApprovalStatus != readiness.ApprovalRevoked ||
		revokedPreflight.ApplyDecision != readiness.ApplyDecisionBlocked || revokedPreflight.ExecutionAvailable {
		t.Fatalf("revoked preflight = %#v", revokedPreflight)
	}
	if _, appErr := New(&countingRunner{}).RevokeApproval(workspace, workspace, granted.ApprovalID, evidence, fixedClock(revokedAt.Add(time.Minute))); appErr == nil || appErr.Code != "UPMCTL_APPROVAL_REVOKED" {
		t.Fatalf("second RevokeApproval error = %#v, want UPMCTL_APPROVAL_REVOKED", appErr)
	}
}

func TestGrantApprovalRechecksExpiryAfterPreflight(t *testing.T) {
	workspace, kubeconfig := managedPlanWorkspace(t)
	createdAt := time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC)
	created, appErr := New(newPlanFixtureRunner(kubeconfig)).PlanVMStart(context.Background(), workspace, workspace, "k8s-3", createdAt)
	if appErr != nil {
		t.Fatalf("PlanVMStart() error = %#v", appErr)
	}
	clock := &sequenceClock{values: []time.Time{createdAt.Add(29*time.Minute + 59*time.Second), createdAt.Add(30 * time.Minute)}}
	_, appErr = New(newPlanFixtureRunner(kubeconfig)).GrantApproval(context.Background(), workspace, workspace, created.PlanID, approvalEvidence(t, "too late"), clock.Now)
	if appErr == nil || appErr.Code != "UPMCTL_PLAN_INVALID" {
		t.Fatalf("GrantApproval() error = %#v, want UPMCTL_PLAN_INVALID", appErr)
	}
	if _, err := approval.NewStore(workspace).ReadByPlan(created.PlanID); !errors.Is(err, approval.ErrApprovalNotFound) {
		t.Fatalf("Approval after expiry race = %v, want not found", err)
	}
}

func TestConcurrentGrantApprovalPublishesExactlyOne(t *testing.T) {
	workspace, kubeconfig := managedPlanWorkspace(t)
	createdAt := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	created, appErr := New(newPlanFixtureRunner(kubeconfig)).PlanVMStart(context.Background(), workspace, workspace, "k8s-3", createdAt)
	if appErr != nil {
		t.Fatalf("PlanVMStart() error = %#v", appErr)
	}

	start := make(chan struct{})
	results := make(chan *Error, 2)
	evidence := approvalEvidence(t, "one approval only")
	var wg sync.WaitGroup
	for index := 0; index < 2; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, appErr := New(newPlanFixtureRunner(kubeconfig)).GrantApproval(context.Background(), workspace, workspace, created.PlanID, evidence, fixedClock(createdAt.Add(time.Minute)))
			results <- appErr
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for result := range results {
		if result == nil {
			successes++
		} else if result.Code == "UPMCTL_APPROVAL_EXISTS" {
			conflicts++
		} else {
			t.Fatalf("unexpected concurrent error = %#v", result)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes/conflicts = %d/%d, want 1/1", successes, conflicts)
	}
	items, err := approval.NewStore(workspace).List()
	if err != nil || len(items) != 1 {
		t.Fatalf("stored Approvals = %#v, %v", items, err)
	}
}

func TestRevokeApprovalRecognizesClaimConflict(t *testing.T) {
	workspace, kubeconfig := managedPlanWorkspace(t)
	createdAt := time.Date(2026, 7, 17, 11, 0, 0, 0, time.UTC)
	created, appErr := New(newPlanFixtureRunner(kubeconfig)).PlanVMStart(context.Background(), workspace, workspace, "k8s-3", createdAt)
	if appErr != nil {
		t.Fatalf("PlanVMStart() error = %#v", appErr)
	}
	granted, appErr := New(newPlanFixtureRunner(kubeconfig)).GrantApproval(context.Background(), workspace, workspace, created.PlanID, approvalEvidence(t, "approve before claim"), fixedClock(createdAt.Add(time.Minute)))
	if appErr != nil {
		t.Fatalf("GrantApproval() error = %#v", appErr)
	}
	claimAt := createdAt.Add(2 * time.Minute)
	claim, err := admission.NewPlanClaim(created, granted, admission.ActorObservation{
		Subject: "os-user:1000", UID: "1000", Username: "operator", Hostname: "test-host",
		Source: approval.SourceHumanCLI, AuthMethod: approval.AuthMethodInteractiveTTY,
	}, admission.AdmissionBasis{
		PlanValidation: admission.AdmissionPlanValid, ApprovalValidation: admission.AdmissionApprovalApproved,
		EnvironmentValidation: admission.AdmissionEnvironmentMatch, DriftValidation: admission.AdmissionDriftMatch,
		CheckedAt: claimAt.Add(-time.Second).Format(time.RFC3339Nano),
	}, nil, claimAt)
	if err != nil {
		t.Fatalf("NewPlanClaim() error = %v", err)
	}
	if _, err := admission.NewStore(workspace).Save(admission.ClaimArtifact(claim)); err != nil {
		t.Fatalf("save Claim: %v", err)
	}
	_, appErr = New(&countingRunner{}).RevokeApproval(workspace, workspace, granted.ApprovalID, approvalEvidence(t, "cannot revoke claim"), fixedClock(createdAt.Add(3*time.Minute)))
	if appErr == nil || appErr.Code != "UPMCTL_PLAN_ALREADY_CLAIMED" {
		t.Fatalf("RevokeApproval() error = %#v, want UPMCTL_PLAN_ALREADY_CLAIMED", appErr)
	}
	inspection, appErr := New(&countingRunner{}).GetApproval(workspace, workspace, granted.ApprovalID, createdAt.Add(3*time.Minute))
	if appErr != nil {
		t.Fatalf("GetApproval() error = %#v", appErr)
	}
	if inspection.Status != readiness.ApprovalInvalid || inspection.Revocation != nil {
		t.Fatalf("claimed inspection = %#v, want INVALID without Revocation", inspection)
	}
}

type sequenceClock struct {
	mu     sync.Mutex
	values []time.Time
	next   int
}

func (c *sequenceClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.next >= len(c.values) {
		return c.values[len(c.values)-1]
	}
	value := c.values[c.next]
	c.next++
	return value
}

func fixedClock(value time.Time) func() time.Time { return func() time.Time { return value } }

func approvalEvidence(t *testing.T, reason string) ApprovalEvidence {
	t.Helper()
	digest, err := upmdigest.Sum("CONFIRM-TEST")
	if err != nil {
		t.Fatal(err)
	}
	return ApprovalEvidence{
		Reason: reason, Terminal: "/dev/tty", ChallengeDigest: digest,
		RequestID: "request-test", CLIVersion: "0.1.0-test",
	}
}
