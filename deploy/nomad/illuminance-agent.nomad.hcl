job "illuminance-agent" {
  datacenters = ["dc1"]
  type        = "service"

  group "illuminance" {
    count = 1

    network {
      port "health" {
        to = 8080
      }
    }

    task "illuminance-agent" {
      driver = "raw_exec"

      artifact {
        source      = "http://artifacts.internal/jeeves/illuminance-agent-${attr.kernel.name}-${attr.cpu.arch}"
        destination = "local/"
        mode        = "file"
      }

      vault {
        policies = ["jeeves-illuminance"]
      }

      template {
        data = <<EOH
JEEVES_MQTT_USER={{ with secret "secret/data/jeeves/mqtt" }}{{ .Data.data.username }}{{ end }}
JEEVES_MQTT_PASSWORD={{ with secret "secret/data/jeeves/mqtt" }}{{ .Data.data.password }}{{ end }}
JEEVES_MQTT_BROKER=mqtt.service.consul
JEEVES_MQTT_PORT=1883
JEEVES_REDIS_HOST=redis.service.consul
JEEVES_REDIS_PORT=6379
JEEVES_LOG_LEVEL=info
JEEVES_SERVICE_NAME=illuminance-agent
EOH
        destination = "secrets/jeeves.env"
        env         = true
      }

      config {
        command = "local/illuminance-agent-${attr.kernel.name}-${attr.cpu.arch}"
        args    = [
          "-health-port", "${NOMAD_PORT_health}",
          "-log-level", "info"
        ]
      }

      resources {
        cpu    = 100
        memory = 128
      }

      service {
        name = "illuminance-agent"
        port = "health"
        tags = ["jeeves", "illuminance"]

        check {
          type     = "http"
          path     = "/health"
          interval = "10s"
          timeout  = "2s"
        }
      }
    }
  }
}
