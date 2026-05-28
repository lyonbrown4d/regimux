package api

import "github.com/gofiber/fiber/v2"

// FiberRoute mounts routes directly on the Fiber app for UI or framework-native
// handlers that should not be exposed through the OpenAPI/httpx path.
type FiberRoute interface {
	RegisterFiber(app *fiber.App)
}
