API_DIR        := api
MIGRATIONS     := file://deploy/migrations
REGISTRY       ?= peter123ouob
TAG            ?= latest
PLATFORM       ?= linux/$(shell go env GOARCH)

SERVICES := api-gateway user-service seckill-service

TARGET_api-gateway     := service/api-gateway
TARGET_user-service    := service/user-service/cmd
TARGET_seckill-service := service/seckill-service/cmd

K8S_OVERLAY ?= deploy/k8s/overlays/local

.PHONY: build test lint fmt tidy migrate-up migrate-down migrate-version images infra-up infra-down k8s-load k8s-build k8s-apply k8s-delete

build:
	go build ./...

test:
	go test ./... -race -count=1

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

migrate-up:
	go run ./cmd/migrate -source $(MIGRATIONS) -direction up

migrate-down:
	go run ./cmd/migrate -source $(MIGRATIONS) -direction down -steps 1

migrate-version:
	go run ./cmd/migrate -source $(MIGRATIONS) -direction version

images: $(addprefix image-,$(SERVICES))

image-%:
	docker buildx build --platform $(PLATFORM) --load \
		--build-arg TARGET=$(TARGET_$*) \
		-f deploy/docker/Dockerfile \
		-t $(REGISTRY)/$*:$(TAG) .

infra-up:
	docker compose -f deploy/docker/docker-compose.yaml up -d

infra-down:
	docker compose -f deploy/docker/docker-compose.yaml down -v

k8s-load:
	eval $$(minikube docker-env) && $(MAKE) images REGISTRY=$(REGISTRY) TAG=$(TAG)

k8s-build:
	kubectl kustomize $(K8S_OVERLAY)

k8s-apply:
	kubectl apply -k $(K8S_OVERLAY)

k8s-delete:
	kubectl delete -k $(K8S_OVERLAY)

gen-%:
	protoc --proto_path=$(API_DIR)/$* \
	       --go_out=$(API_DIR)/$* --go_opt=paths=source_relative \
	       --go-grpc_out=$(API_DIR)/$* --go-grpc_opt=paths=source_relative \
	       $(API_DIR)/$*/*.proto
