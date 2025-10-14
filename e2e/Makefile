.PHONY: help build test test-all clean logs start stop

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-15s %s\n", $$1, $$2}'

build: ## Build test infrastructure Docker images
	docker-compose -f docker-compose.test.yml build

test: ## Run a single test scenario (usage: make test SCENARIO=hallway_passthrough)
	@if [ -z "$(SCENARIO)" ]; then \
		echo "Error: SCENARIO is required. Usage: make test SCENARIO=hallway_passthrough"; \
		exit 1; \
	fi
	./run-test.sh $(SCENARIO)

test-all: ## Run all test scenarios
	./run-all-tests.sh

list: ## List available test scenarios
	@echo "Available test scenarios:"
	@ls -1 ../test-scenarios/*.yaml | xargs -n1 basename -s .yaml

clean: ## Remove test output and stop containers
	docker-compose -f docker-compose.test.yml down -v
	rm -rf test-output/*

clean-all: clean ## Remove test output, stop containers, and remove images
	docker-compose -f docker-compose.test.yml down -v --rmi all

logs: ## Show logs from all containers (usage: make logs SERVICE=occupancy-agent)
	@if [ -z "$(SERVICE)" ]; then \
		docker-compose -f docker-compose.test.yml logs -f; \
	else \
		docker-compose -f docker-compose.test.yml logs -f $(SERVICE); \
	fi

start: ## Start test infrastructure (without running tests)
	docker-compose -f docker-compose.test.yml up -d mosquitto redis collector illuminance-agent light-agent occupancy-agent behavior-agent

stop: ## Stop all test infrastructure
	docker-compose -f docker-compose.test.yml down

restart: ## Restart all test infrastructure
	docker-compose -f docker-compose.test.yml restart

observer: ## Run standalone observer for debugging
	docker-compose -f docker-compose.test.yml up observer

shell-redis: ## Open Redis CLI
	docker exec -it jeeves-test-redis redis-cli

shell-mqtt: ## Open Mosquitto shell
	docker exec -it jeeves-test-mosquitto sh

publish: ## Publish a test MQTT message (usage: make publish TOPIC=sensor/motion/test VALUE='{"value":true}')
	@if [ -z "$(TOPIC)" ] || [ -z "$(VALUE)" ]; then \
		echo "Error: TOPIC and VALUE are required."; \
		echo "Usage: make publish TOPIC=sensor/motion/test VALUE='{\"value\":true}'"; \
		exit 1; \
	fi
	docker exec -it jeeves-test-mosquitto mosquitto_pub -t "$(TOPIC)" -m '$(VALUE)'

view-timeline: ## View timeline for a scenario (usage: make view-timeline SCENARIO=hallway_passthrough)
	@if [ -z "$(SCENARIO)" ]; then \
		echo "Error: SCENARIO is required."; \
		exit 1; \
	fi
	cat test-output/timelines/$(SCENARIO).txt

view-capture: ## View MQTT capture for a scenario (usage: make view-capture SCENARIO=hallway_passthrough)
	@if [ -z "$(SCENARIO)" ]; then \
		echo "Error: SCENARIO is required."; \
		exit 1; \
	fi
	cat test-output/captures/$(SCENARIO).json | jq

verify: ## Verify prerequisites are installed
	@echo "Checking prerequisites..."
	@command -v docker >/dev/null 2>&1 || { echo "❌ Docker is not installed"; exit 1; }
	@echo "✅ Docker is installed"
	@command -v docker-compose >/dev/null 2>&1 || { echo "❌ Docker Compose is not installed"; exit 1; }
	@echo "✅ Docker Compose is installed"
	@command -v ollama >/dev/null 2>&1 || { echo "❌ Ollama is not installed"; exit 1; }
	@echo "✅ Ollama is installed"
	@curl -s http://localhost:11434/api/tags >/dev/null 2>&1 || { echo "❌ Ollama is not running"; exit 1; }
	@echo "✅ Ollama is running"
	@curl -s http://localhost:11434/api/tags | grep -q "mixtral:8x7b" || { echo "⚠️  mixtral:8x7b model not found. Run: ollama pull mixtral:8x7b"; }
	@echo "✅ All prerequisites satisfied"

status: ## Show status of test infrastructure
	docker-compose -f docker-compose.test.yml ps

health: ## Check health of all services
	@echo "Checking service health..."
	@docker-compose -f docker-compose.test.yml ps | grep -q "healthy" && echo "✅ Services are healthy" || echo "⚠️  Some services may not be healthy"
