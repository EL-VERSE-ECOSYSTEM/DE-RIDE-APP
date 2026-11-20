package main

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"github.com/deride/ecosystem/backend/internal/cache"
	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("connect to Postgres: %v", err)
	}
	defer db.Close()
	if err := db.Ping(ctx); err != nil {
		log.Fatalf("ping Postgres: %v", err)
	}
	store := cache.NewMemoryStore(30 * time.Second)
	defer store.Close()
	app := fiber.New(fiber.Config{AppName: "DE RIDE API"})
	app.Get("/healthz", func(c *fiber.Ctx) error { return c.JSON(fiber.Map{"status": "ok", "database": "connected"}) })
	app.Get("/api/v1/health", func(c *fiber.Ctx) error {
		probeCtx, probeCancel := context.WithTimeout(c.Context(), 2*time.Second)
		defer probeCancel()
		if err := db.Ping(probeCtx); err != nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"status": "degraded", "database": "unavailable"})
		}
		return c.JSON(fiber.Map{"status": "ok", "database": "connected"})
	})
	app.Post("/api/v1/users/switch-rider-mode", func(c *fiber.Ctx) error {
		return c.Status(501).JSON(fiber.Map{"error": "NOT_CONFIGURED", "message": "Authentication and persistence are required for rider mode switching"})
	})
	app.Get("/ws", func(c *fiber.Ctx) error {
		return c.Status(426).SendString("websocket endpoint requires authenticated upgrade")
	})
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}
	log.Fatal(app.Listen(":" + port))
}
