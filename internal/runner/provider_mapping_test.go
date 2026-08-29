package runner

import (
	"testing"

	runnerv1 "github.com/Lin-xun1113/SkillGate/gen/go/runner/v1"
	"github.com/Lin-xun1113/SkillGate/internal/retry"
)

func TestProviderFailureCategoryMappingIsStable(t *testing.T) {
	tests := []struct {
		name     string
		category runnerv1.FailureCategory
		want     retry.Category
		retry    bool
	}{
		{"transient", runnerv1.FailureCategory_FAILURE_CATEGORY_TRANSIENT, retry.ProviderTransient, true},
		{"timeout", runnerv1.FailureCategory_FAILURE_CATEGORY_TIMEOUT, retry.LeaseTimeout, true},
		{"permanent", runnerv1.FailureCategory_FAILURE_CATEGORY_PERMANENT, retry.DeterministicFailure, false},
		{"internal", runnerv1.FailureCategory_FAILURE_CATEGORY_INTERNAL, retry.WorkerLost, true},
		{"sandbox", runnerv1.FailureCategory_FAILURE_CATEGORY_SANDBOX_ERROR, retry.DependencyTransient, true},
		{"unspecified", runnerv1.FailureCategory_FAILURE_CATEGORY_UNSPECIFIED, retry.DeterministicFailure, false},
	}
	policy := retry.DefaultPolicy()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := mapFailureCategory(test.category)
			if got != test.want {
				t.Fatalf("mapFailureCategory(%s) = %q, want %q", test.category, got, test.want)
			}
			if policy.Allows(got, 1) != test.retry {
				t.Fatalf("retry policy Allows(%q) = %v, want %v", got, policy.Allows(got, 1), test.retry)
			}
		})
	}
}
