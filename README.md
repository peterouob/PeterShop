# TODO
- [ ] full chain on the sec-kill(include Idempotence)
- [ ] rate limit
- [ ] TCC
- [ ] roll back and compensate
- [ ] chaos and k6/jmeter
- [ ] LGTM(Loki,Grafana,Tempo,Mimir)

### How to run

## Step1. Build the env

```bash
k apply -k ./deploy/k8s/base/local
```

## Step2. Run migration

```bash
go run ./cmd/migrate/main.go
```

## Step3. Forward the port

```bash
k port-forward svc/api-gateway 8081:8080 -n peter-shop
```

## Step4. Test the api

```bash
curl -X POST http://localhost:8081/api/v1/users/login
```
