.PHONY: dev
dev:
	skaffold dev --namespace cloudflare-tunnel-ingress-controller-dev

.PHONY: login
login:
	echo "${GITHUB_PAT}" | docker login ghcr.io -u khageshsaini --password-stdin
	echo "${GITHUB_PAT}" | helm registry login ghcr.io/khageshsaini -u khageshsaini --password-stdin

.PHONY: image
image:
	DOCKER_BUILDKIT=1 TARGETARCH=amd64 docker build --platform linux/amd64 -t ghcr.io/khageshsaini/cloudflare-tunnel-ingress-controller -f ./image/cloudflare-tunnel-ingress-controller/Dockerfile . 

.PHONY: push-image
push-image:
	DOCKER_BUILDKIT=1 docker push ghcr.io/khageshsaini/cloudflare-tunnel-ingress-controller

.PHONY: build-chart
build-chart:
	rm -rf  *.tgz
	@echo "Enter the chart version (e.g., 0.0.19):"
	@read version; \
	helm package helm/cloudflare-tunnel-ingress-controller --version $$version

.PHONY: push-chart
push-chart:
	@echo "Enter the chart version (e.g., 0.0.19):"
	@read version; \
	helm push cloudflare-tunnel-ingress-controller-$$version.tgz oci://ghcr.io/khageshsaini/helm-charts

.PHONY: unit-test
unit-test:
	CGO_ENABLED=1 go test -race ./pkg/... -coverprofile ./cover.out

.PHONY: integration-test
integration-test: setup-envtest
	KUBEBUILDER_ASSETS="$(shell setup-envtest use $(ENVTEST_K8S_VERSION) -p path)" CGO_ENABLED=1 go test -race -v -coverpkg=./... -coverprofile ./test/integration/cover.out ./test/integration/...

.PHONY: setup-envtest
setup-envtest:
	bash ./hack/install-setup-envtest.sh
