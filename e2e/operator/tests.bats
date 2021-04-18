#!/usr/bin/env bats

NAMESPACE=${NAMESPACE:-default}

printInfo() {
	printf '\nEVENTS:\n'
	kubectl -n $NAMESPACE get events | sed -E 's/^/  /g'
	printf '\nTEST POD LOGS:\n'
	kubectl -n $NAMESPACE logs $(kubectl get pod -o name | grep "/$1" | sed 's/pod\///') 2>/dev/null | sed -E 's/^/  /g'
}

runTestPod() {
	kubectl -n $NAMESPACE apply -f $BATS_TEST_DIRNAME/${1}-job.yaml
	kubectl -n $NAMESPACE wait --for condition=complete --timeout=20s job/${1}-job || (printInfo $1; false)
}

runTestPodAndWaitForPVCDeletion() {
	runTestPod $1
	waitForPVDeletion $1
}

deleteTestPod() {
	PVC_UID=$(kubectl -n $NAMESPACE get pvc ${1}-pvc -o jsonpath='{.metadata.uid}')
	kubectl -n $NAMESPACE delete --timeout 20s -f $BATS_TEST_DIRNAME/${1}-job.yaml
	waitForPVDeletion $1
}

waitForPVDeletion() {
	kubectl -n $NAMESPACE wait --for delete --timeout=20s pvc/${1}-pvc || ! kubectl -n $NAMESPACE get pvc ${1}-pvc 2>/dev/null || (echo PVC ${1}-pvc was not deleted >&2; false)
	kubectl wait --for delete --timeout=20s pv/pvc-$PVC_UID || ! kubectl get pv pvc-$PVC_UID 2>/dev/null || (echo PersistentVolume pvc-$PVC_UID was not deleted >&2; false)
}

teardown() {
	printf '\nTEARDOWN:\n'
	for TEST in hostdir cached1 cached2; do
		kubectl -n $NAMESPACE delete -f $BATS_TEST_DIRNAME/test-${TEST}-job.yaml --ignore-not-found || true
	done
}

@test "provision hostdir volume for Job/PVC" {
	runTestPod test-hostdir
	deleteTestPod test-hostdir
}

@test "provision cache volume for Job/PVC" {
	runTestPodAndWaitForPVCDeletion test-cached1
}

@test "provision cache volume for 2nd Job/PVC to read previously committed contents" {
	runTestPodAndWaitForPVCDeletion test-cached2
}
