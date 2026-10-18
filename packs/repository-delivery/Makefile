PYTHON ?= .venv/bin/python

.PHONY: test quality quality-fast build

test:
	PYTHONPATH=src $(PYTHON) -m unittest discover -s tests -v

quality-fast:
	PYTHONPATH=src $(PYTHON) -m compileall -q src scripts tests
	PYTHONPATH=src $(PYTHON) -m coverage run --source=repo_delivery -m unittest discover -s tests
	$(PYTHON) -m coverage report --fail-under=80

quality: quality-fast
	git diff --check

build:
	$(PYTHON) -m build

.PHONY: test-runtime-integration
test-runtime-integration:
	@test -n "$(REPO_DELIVERY_SWARMD_BINARY)" || (echo "Set REPO_DELIVERY_SWARMD_BINARY to a built swarmd binary" >&2; exit 1)
	REPO_DELIVERY_SWARMD_BINARY="$(REPO_DELIVERY_SWARMD_BINARY)" PYTHONPATH=src $(PYTHON) -m unittest discover -s tests -p test_swarmd_integration.py -v

.PHONY: test-fenced-runtime-integration
test-fenced-runtime-integration:
	@test -n "$(REPO_DELIVERY_SWARMD_BINARY)" || (echo "Set REPO_DELIVERY_SWARMD_BINARY to a built swarmd binary" >&2; exit 1)
	@test -n "$(REPO_DELIVERY_SWARMD_FIXTURE_BINARY)" || (echo "Set REPO_DELIVERY_SWARMD_FIXTURE_BINARY to the attempt fixture producer" >&2; exit 1)
	$(MAKE) test-runtime-integration
