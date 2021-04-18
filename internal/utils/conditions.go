package utils

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetCondition returns the condition of the given type from the list of conditions or nil if it doesn't exist
func GetCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for _, condition := range conditions {
		if condition.Type == condType {
			return &condition
		}
	}
	return nil
}

// SetCondition adds or updates a condition
func SetCondition(conditions *[]metav1.Condition, newCond metav1.Condition) bool {
	newCond.LastTransitionTime = metav1.Time{Time: time.Now()}
	for i, condition := range *conditions {
		if condition.Type == newCond.Type {
			if condition.Status == newCond.Status {
				newCond.LastTransitionTime = condition.LastTransitionTime
			}
			changed := condition.Status != newCond.Status ||
				condition.ObservedGeneration != newCond.ObservedGeneration ||
				condition.Reason != newCond.Reason ||
				condition.Message != newCond.Message
			(*conditions)[i] = newCond
			return changed
		}
	}
	*conditions = append(*conditions, newCond)
	return true
}
