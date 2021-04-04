#!/usr/bin/env bats

printInfo() {
	printf '\nEVENTS:\n'
	kubectl get events | sed -E 's/^/  /g'
	printf '\nTEST POD LOGS:\n'
	kubectl logs $(kubectl get pod -o name | grep '/test-job' | sed 's/pod\///') | sed -E 's/^/  /g'
}

teardown() {
	printf '\nTEARDOWN:\n'
	kubectl delete -f $BATS_TEST_DIRNAME/test-job1.yaml --ignore-not-found || true
	kubectl delete -f $BATS_TEST_DIRNAME/test-job2.yaml --ignore-not-found || true
}

@test "provision volume for Job/PVC" {
	kubectl apply -f $BATS_TEST_DIRNAME/test-job1.yaml
	kubectl wait --for condition=complete --timeout=20s job/test-job1 || (printInfo; false)
	kubectl wait --for delete --timeout=5s pvc/test-pvc1 || ! kubectl get pvc test-pvc1
}

@test "provision volume for 2nd Job/PVC with the previously committed contents" {
	kubectl apply -f $BATS_TEST_DIRNAME/test-job2.yaml
	kubectl wait --for condition=complete --timeout=20s job/test-job2 || (printInfo; false)
	kubectl wait --for delete --timeout=5s pvc/test-pvc2 || ! kubectl get pvc test-pvc2
}
