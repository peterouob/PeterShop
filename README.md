# TODO
- [ ] full chain on the sec-kill(include Idempotence)
- [ ] rate limit
- [ ] TCC
- [ ] roll back and compensate
- [ ] chaos and k6/jmeter
- [ ] LGTM(Loki,Grafana,Tempo,Mimir)

### How to run

目前沒有推送映像到 registry 的 CI, image是在 k8s 直接 build 
所以**每次改完程式都要先跑 Step1 重新 build**，否則k8s上跑的還是舊版本

## Step1. Build the images into minikube

```bash
make k8s-load
# eval $(minikube docker-env) and build the svc image
```

## Step2. Build the env

```bash
make k8s-apply
# kubectl apply -k deploy/k8s/overlays/local
```

## Step3. Run migration

```bash
make migrate-up
# go run ./cmd/migrate/main.go
```
在本機跑，所以 `.env` 裡的 MySQL 位址要能連得到
(k8s MySQL need `kubectl port-forward svc/mysql 3306:3306 -n peter-shop`)

## Step4. Forward the port

```bash
kubectl port-forward svc/api-gateway 8081:8080 -n peter-shop
```

## Step5. Test the api

```bash
curl -X POST http://localhost:8081/api/v1/users/login
```

## 重新部署

```bash
kubectl rollout restart \
  deploy/api-gateway deploy/user-service deploy/seckill-service \
  -n peter-shop
```

## 清除

```bash
make k8s-delete
```
