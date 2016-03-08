#!/bin/bash
#
# This scripts starts the OpenShift server with a default configuration.
# The OpenShift Docker registry and router are installed.
# It will run all tests that are imported into test/extended.

set -o errexit
set -o nounset
set -o pipefail

OS_ROOT=$(dirname "${BASH_SOURCE}")/../../..
source "${OS_ROOT}/hack/util.sh"
source "${OS_ROOT}/hack/common.sh"
source "${OS_ROOT}/hack/lib/log.sh"
os::log::install_errexit

cd "${OS_ROOT}"

# build binaries
if [[ -z $(os::build::find-binary ginkgo) ]]; then
  hack/build-go.sh Godeps/_workspace/src/github.com/onsi/ginkgo/ginkgo
fi
if [[ -z $(os::build::find-binary localquota.test) ]]; then
  hack/build-go.sh test/extended/localquota/localquota.test
fi
if [[ -z $(os::build::find-binary openshift) ]]; then
  hack/build-go.sh
fi
ginkgo="$(os::build::find-binary ginkgo)"
localquotatest="$(os::build::find-binary localquota.test)"

source "${OS_ROOT}/hack/lib/util/environment.sh"
os::util::environment::setup_time_vars

if [[ -z ${TEST_ONLY+x} ]]; then
  ensure_iptables_or_die

  function cleanup()
  {
    out=$?
    cleanup_openshift
    echo "[INFO] Exiting"
    return $out
  }

  trap "exit" INT TERM
  trap "cleanup" EXIT
  echo "[INFO] Starting server"

  os::util::environment::setup_all_server_vars "test-extended/core"
  os::util::environment::use_sudo
  reset_tmp_dir

  os::log::start_system_logger

  # when selinux is enforcing, the volume dir selinux label needs to be
  # svirt_sandbox_file_t
  #
  # TODO: fix the selinux policy to either allow openshift_var_lib_dir_t
  # or to default the volume dir to svirt_sandbox_file_t.
  if selinuxenabled; then
         sudo chcon -t svirt_sandbox_file_t ${VOLUME_DIR}
  fi
  configure_os_server
  start_os_server

  export KUBECONFIG="${ADMIN_KUBECONFIG}"

  echo "[INFO] Node config"
  cat $NODE_CONFIG_DIR/node-config.yaml
else
  # be sure to set VOLUME_DIR if you are running with TEST_ONLY
  echo "[INFO] Not starting server, VOLUME_DIR=${VOLUME_DIR:-}"
fi

# ensure proper relative directories are set
export TMPDIR=${BASETMPDIR:-/tmp}
export EXTENDED_TEST_PATH="$(pwd)/test/extended"


# run tests in serial
echo "[INFO] Running serial tests"
echo ${ginkgo}
echo "ALL DONE?"
${ginkgo} -v  ${localquotatest} -- -ginkgo.v -test.timeout 2m

