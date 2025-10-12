job "occupancy-agent" {
  datacenters = ["dc1"]
  type        = "service"

  group "occupancy" {
    count = 1

    network {
      port "health" {
        to = 8080
      }
    }

    task "occupancy-agent" {
      driver = "raw_exec"

      artifact {
        source      = "http://artifacts.internal/jeeves/occupancy-agent-${attr.kernel.name}-${attr.cpu.arch}"
        destination = "local/"
        mode        = "file"
      }

      vault {
        policies = ["jeeves-occupancy"]
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
JEEVES_SERVICE_NAME=occupancy-agent
EOH
        destination = "secrets/jeeves.env"
        env         = true
      }

      config {
        command = "local/occupancy-agent-${attr.kernel.name}-${attr.cpu.arch}"
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
        name = "occupancy-agent"
        port = "health"
        tags = ["jeeves", "occupancy"]

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
