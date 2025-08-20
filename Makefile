.PHONY: test

BASE_URL=http://localhost:80
TENANT1=tenant1
TENANT2=tenant2

test:
	@echo "==================== 1) Healthcheck ===================="; \
	curl -s -w "\nHTTP %{http_code}\n" $(BASE_URL)/healthz; \
	echo; \
	\
	echo "==================== 2) GET /kv sem key ===================="; \
	curl -s -w "\nHTTP %{http_code}\n" -H "X-Tenant-Id: $(TENANT1)" "$(BASE_URL)/kv"; \
	echo; \
	\
	echo "==================== 3) GET /kv com key (gera valor) ===================="; \
	curl -s -w "\nHTTP %{http_code}\n" -H "X-Tenant-Id: $(TENANT1)" "$(BASE_URL)/kv?key=foo"; \
	echo; \
	\
	echo "==================== 4) GET /kv com key (cache hit) ===================="; \
	curl -s -w "\nHTTP %{http_code}\n" -H "X-Tenant-Id: $(TENANT1)" "$(BASE_URL)/kv?key=foo"; \
	echo; \
	\
	echo "==================== 5) POST /kv (sobrescreve valor) ===================="; \
	curl -s -w "\nHTTP %{http_code}\n" -X POST \
	     -H "X-Tenant-Id: $(TENANT1)" \
	     -d "key=foo" -d "val=bar" \
	     $(BASE_URL)/kv; \
	echo; \
	\
	echo "==================== 6) GET /kv (valor sobrescrito) ===================="; \
	curl -s -w "\nHTTP %{http_code}\n" -H "X-Tenant-Id: $(TENANT1)" "$(BASE_URL)/kv?key=foo"; \
	echo; \
	\
	echo "==================== 7) GET /whoami ===================="; \
	curl -s -w "\nHTTP %{http_code}\n" $(BASE_URL)/whoami; \
	echo; \
	\
	echo "==================== 8) Multi-tenant test ===================="; \
	echo "---- Tenant1 ----"; \
	curl -s -w "\nHTTP %{http_code}\n" -H "X-Tenant-Id: $(TENANT1)" "$(BASE_URL)/kv?key=shared"; \
	echo; \
	echo "---- Tenant2 ----"; \
	curl -s -w "\nHTTP %{http_code}\n" -H "X-Tenant-Id: $(TENANT2)" "$(BASE_URL)/kv?key=shared"; \
	echo
