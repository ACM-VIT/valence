SHELL := /bin/bash
.DEFAULT_GOAL := help

# Recipes load .env themselves (instead of `include .env`), so values with
# characters make treats specially, like $ or #, reach the program unchanged.
LOAD_ENV := set -a; [ -f .env ] && . ./.env; set +a

PROBLEMS := $(wildcard problems/examples/*)

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: validate
validate: ## Check every example problem package without touching the database
	go run ./services/api/cmd/valence-admin problem validate $(PROBLEMS)

.PHONY: seed
seed: ## Import every example problem into DATABASE_URL and the blob store (BLOB_DIR, default .data/blobs)
	$(LOAD_ENV); go run ./services/api/cmd/valence-admin problem import $(PROBLEMS)
