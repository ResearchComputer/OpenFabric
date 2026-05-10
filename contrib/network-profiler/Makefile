.PHONY: test bench-smoke

test:
	python3 -m pytest tests/ -v

bench-smoke:
	cd ../../src && $(MAKE) build
	python3 -m pytest tests/test_bench_smoke.py -v -s
