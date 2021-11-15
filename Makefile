GO_INSTALL = ./hack/go_install.sh

TOOLS_DIR := $(abspath hack/tools)

CONTROLLER_GEN_VER := v0.5.0
CONTROLLER_GEN_BIN := controller-gen
CONTROLLER_GEN := $(TOOLS_DIR)/$(CONTROLLER_GEN_BIN)-$(CONTROLLER_GEN_VER)

OPENSHIFT_GOIMPORTS_VER := main
OPENSHIFT_GOIMPORTS_BIN := openshift-goimports
OPENSHIFT_GOIMPORTS := $(TOOLS_DIR)/$(OPENSHIFT_GOIMPORTS_BIN)-$(OPENSHIFT_GOIMPORTS_VER)

all: build
.PHONY: all

build:
	go build -o bin ./cmd/...
.PHONY: build

vendor:
	go mod tidy
	go mod vendor
.PHONY: vendor

$(CONTROLLER_GEN):
	GOBIN=$(TOOLS_DIR) $(GO_INSTALL) sigs.k8s.io/controller-tools/cmd/controller-gen $(CONTROLLER_GEN_BIN) $(CONTROLLER_GEN_VER)

$(OPENSHIFT_GOIMPORTS):
	GOBIN=$(TOOLS_DIR) $(GO_INSTALL) github.com/coreydaley/openshift-goimports $(OPENSHIFT_GOIMPORTS_BIN) $(OPENSHIFT_GOIMPORTS_VER)

.PHONY: codegen
codegen: $(CONTROLLER_GEN)
	./hack/update-codegen.sh


.PHONY: imports
imports: $(OPENSHIFT_GOIMPORTS)
	$(OPENSHIFT_GOIMPORTS) -m github.com/kcp-dev/kcp

.PHONY: clean-tools
clean-tools:
	rm -rf $(TOOLS_DIR)