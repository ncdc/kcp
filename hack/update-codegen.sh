#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail


HACK_DIR=$(dirname "${BASH_SOURCE[0]}")
TOOLS_DIR=$(cd "${HACK_DIR}/tools"; pwd -P)
KCP_DIR="${HACK_DIR}/.."

CONTROLLER_GEN="${HACK_DIR}/tools/controller-gen"
if [[ ! -x ${CONTROLLER_GEN} ]]; then
    echo "Please run via 'make codegen' (${CONTROLLER_GEN} is missing or not executable)"
    exit 1
fi

# For now, use the generate-groups.sh script from the kcp-dev fork of kubernetes, as there may
# be changes to some of the generators. Requires a clone as a sibling to kcp.
CODEGEN_PKG=${CODEGEN_PKG:-${KCP_DIR}/../kubernetes/staging/src/k8s.io/code-generator}
# GENERATE_GROUPS="${GENERATE_GROUPS:-${CODEGEN_PKG}/generate-groups.sh}"
# if [[ ! -x ${GENERATE_GROUPS} ]]; then
#     echo "Unable to find executable generate-groups.sh script at: ${GENERATE_GROUPS}"
#     exit 1
# fi

#bash "${GENERATE_GROUPS}" "deepcopy,client,informer,lister" \
#  ./pkg/client github.com/kcp-dev/kcp/pkg/apis \
 # "cluster:v1alpha1 apiresource:v1alpha1 tenancy:v1alpha1" \
  #--go-header-file "${HACK_DIR}"/boilerplate.go.txt

(
    cd "${CODEGEN_PKG}"
    GOBIN="${TOOLS_DIR}" go install ./cmd/{client-gen,deepcopy-gen,lister-gen,informer-gen}
)

MODULE=github.com/kcp-dev/kcp

APIS=(
    cluster/v1alpha1
    apiresource/v1alpha1
    tenancy/v1alpha1
)

function join() {
    local IFS=','
    echo "github.com/kcp-dev/kcp/pkg/apis/$*"
}

FQ_APIS=$(join "${APIS[@]}")

"${TOOLS_DIR}/deepcopy-gen" --input-dirs "${FQ_APIS}" \
    -O zz_generated.deepcopy \
    --bounding-dirs "${MODULE}"/pkg/apis \
    --go-header-file "${HACK_DIR}"/boilerplate.go.txt \
    --trim-path-prefix "${MODULE}"

"${TOOLS_DIR}/client-gen" --clientset-name versioned \
    --input-base "" \
    --input "${FQ_APIS}" \
    --output-package "${MODULE}/pkg/client/clientset" \
    --go-header-file "${HACK_DIR}"/boilerplate.go.txt \
    --trim-path-prefix "${MODULE}"

"${TOOLS_DIR}/lister-gen" \
    --input-dirs "${FQ_APIS}" \
    --output-package "${MODULE}/pkg/client/listers" \
    --go-header-file "${HACK_DIR}"/boilerplate.go.txt \
    --trim-path-prefix "${MODULE}"

"${TOOLS_DIR}/informer-gen" \
    --input-dirs "${FQ_APIS}" \
    --versioned-clientset-package "${MODULE}/pkg/client/clientset/versioned" \
    --listers-package "${MODULE}/pkg/client/listers" \
    --output-package "${MODULE}/pkg/client/informers" \
    --go-header-file "${HACK_DIR}"/boilerplate.go.txt \
    --trim-path-prefix "${MODULE}"

# Update generated CRD YAML
"${CONTROLLER_GEN}" \
    crd \
    rbac:roleName=manager-role \
    webhook \
    paths="./..." \
    output:crd:artifacts:config=config/
