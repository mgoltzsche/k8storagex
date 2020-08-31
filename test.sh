#!/bin/sh

VOLNAME=pv1

# ARGS: SCRIPTNAME
runScript() {
	./dockerizedfs.sh $1 $VOLNAME
}

set -ex

mkdir -p testdata
rm -rf testdata/pv1

echo '## TEST setup #################################'

runScript setup

EXPECTED_CONTENT="testcontent $(date)"
echo "$EXPECTED_CONTENT" > testdata/$VOLNAME/testfile

echo '## TEST teardown ##############################'

runScript teardown

[ ! -d testdata/$VOLNAME ] || (echo fail: volume should be removed >&2; false)

echo '## TEST restore ################################'

runScript setup

CONTENT="$(cat testdata/$VOLNAME/testfile)"
[ "$CONTENT" = "$EXPECTED_CONTENT" ] || (echo fail: volume should return what was last written into that cache key >&2; false)

runScript teardown
