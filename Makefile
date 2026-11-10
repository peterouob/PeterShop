-include .env

API_DIR        := api
DBS            := user seckill order payment
REGISTRY       ?= peter123ouob
TAG            ?= latest
PLATFORM       ?= linux/$(shell go env GOARCH)
SVC_PREFIX 	   ?= deploy/
SVC_ALL        ?= deploy/api-gateway deploy/user-service deploy/seckill-service deploy/order-service

SERVICES := api-gateway user-service seckill-service order-service

TARGET_api-gateway     := service/api-gateway
TARGET_user-service    := service/user-service/cmd
TARGET_seckill-service := service/seckill-service/cmd
TARGET_order-service   := service/order-service/cmd

K8S_OVERLAY ?= deploy/k8s/overlays/local
K8S_NS      ?= peter-shop

.PHONY: tidy fmt lint test build \
        migrate-up migrate-version \
        images \
        infra-up infra-down \
        k8s-load k8s-build k8s-apply k8s-delete k8s-update verify-cdc \
        seed bootstrap


tidy:
	go mod tidy

fmt: tidy
	gofmt -w .

lint: fmt
	golangci-lint run ./...

test: lint
	go test ./... -race -count=1

build:
	go build ./...

PGHOST ?= localhost
PGPORT ?= 5433
N      ?= 1

# usage: make migrate-up | migrate-up-seckill | migrate-down-order N=2 | migrate-create-seckill NAME=add_index
MIGRATE = migrate -path deploy/migrations/$* \
          -database "postgres://svc_$*:$(or $(PGPASSWORD_$*),password)@$(PGHOST):$(PGPORT)/db_$*?sslmode=disable"

migrate-up: $(addprefix migrate-up-,$(DBS))
migrate-version: $(addprefix migrate-version-,$(DBS))

migrate-up-%:
	$(MIGRATE) up

migrate-down-%:
	$(MIGRATE) down $(N)

migrate-version-%:
	@printf '%-8s ' $*; $(MIGRATE) version

migrate-create-%:
	@test -n "$(NAME)" || (echo "usage: make migrate-create-$* NAME=xxx" && exit 1)
	migrate create -ext sql -dir deploy/migrations/$* -seq -digits 7 $(NAME)

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
	$(MAKE) images REGISTRY=seckill TAG=local
	$(foreach s,$(SERVICES),minikube image load seckill/$(s):local &&) true

k8s-load-%:
	$(MAKE) image-$* REGISTRY=seckill TAG=local
	minikube image load seckill/$*:local

k8s-build:
	kubectl kustomize $(K8S_OVERLAY)

k8s-apply:
	kubectl apply -k $(K8S_OVERLAY)

k8s-delete:
	kubectl delete -k $(K8S_OVERLAY)

k8s-update-%:
	kubectl -n $(K8S_NS) rollout restart $(SVC_PREFIX)$*

k8s-update:
	kubectl -n $(K8S_NS) rollout restart $(SVC)

k8s-redeploy: k8s-load
	kubectl -n $(K8S_NS) rollout restart $(SVC_ALL)

gen-%:
	protoc --proto_path=$(API_DIR)/$* \
	       --go_out=$(API_DIR)/$* --go_opt=paths=source_relative \
	       --go-grpc_out=$(API_DIR)/$* --go-grpc_opt=paths=source_relative \
	       $(API_DIR)/$*/*.proto

PSQL_SECKILL = kubectl exec -n $(K8S_NS) deploy/postgres -- psql -U postgres -d db_seckill -tAc

verify-cdc:
	@test "$$($(PSQL_SECKILL) 'SHOW wal_level')" = logical || (echo "✗ wal_level is not logical" && exit 1)
	@test "$$($(PSQL_SECKILL) "SELECT has_table_privilege('svc_cdc','outbox','SELECT')")" = t || (echo "✗ svc_cdc cannot read outbox" && exit 1)
	@test "$$($(PSQL_SECKILL) "SELECT has_table_privilege('svc_cdc','stock_ledger','SELECT')")" = f || (echo "✗ svc_cdc can read stock_ledger" && exit 1)
	@echo "cdc ready"

KAFKA_TOPICS = kubectl exec -n $(K8S_NS) deploy/kafka -- /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092

kafka-topics:
	$(KAFKA_TOPICS) --create --if-not-exists --topic seckill-order --partitions 3 --replication-factor 1
	$(KAFKA_TOPICS) --create --if-not-exists --topic seckill-order-dlq --partitions 1 --replication-factor 1
	$(KAFKA_TOPICS) --create --if-not-exists --topic __debezium-heartbeat.seckill --partitions 1 --replication-factor 1
	$(KAFKA_TOPICS) --describe --topic seckill-order
	$(KAFKA_TOPICS) --describe --topic seckill-order-dlq

seed:
	deploy/scripts/seed.sh $(AID) $(STOCK)

bootstrap:
	kubectl -n $(K8S_NS) wait --for=condition=Available deploy/postgres deploy/kafka deploy/redis --timeout=180s
	$(MAKE) migrate-up
	$(MAKE) kafka-topics
	$(MAKE) seed
	kubectl -n $(K8S_NS) rollout restart deploy/debezium
