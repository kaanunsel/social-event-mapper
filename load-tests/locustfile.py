import random
from locust import HttpUser, task, between


DISCOVER_COORDS = [
    (41.0840, 29.0510),  # Bebek / Etiler
    (41.0422, 29.0083),  # Besiktas
    (40.9910, 29.0230),  # Kadikoy
    (41.0369, 28.9850),  # Taksim
]


class WebUser(HttpUser):
    """Simulates browser traffic for the React SPA and API documentation."""

    weight = 1
    wait_time = between(1.0, 3.0)

    @task(4)
    def load_spa(self):
        self.client.get("/", name="web /")

    @task(1)
    def load_api_docs(self):
        self.client.get("/api/docs/", name="web /api/docs/")


class APIUser(HttpUser):
    """Simulates client API traffic for discovery and health endpoints."""

    weight = 3
    wait_time = between(0.5, 2.0)

    @task(5)
    def discover_events(self):
        lat, lon = random.choice(DISCOVER_COORDS)
        self.client.get(
            f"/api/events?lat={lat}&lon={lon}&radius_km=15",
            name="api /api/events",
        )

    @task(2)
    def list_categories(self):
        self.client.get("/api/categories", name="api /api/categories")

    @task(1)
    def health_check(self):
        self.client.get("/api/health", name="api /api/health")
