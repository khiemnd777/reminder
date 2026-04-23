package main

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"

	"reminder/backend/internal/handler"
	"reminder/backend/internal/model"
	"reminder/backend/internal/provider"
	"reminder/backend/internal/repository"
	"reminder/backend/internal/service"
	"reminder/backend/pkg/db"
)

func main() {
	databasePath := getenv("DATABASE_PATH", "./data/reminder.db")
	if err := os.MkdirAll(dirOf(databasePath), 0o755); err != nil {
		log.Fatalf("create data directory: %v", err)
	}

	database, err := db.OpenSQLite(databasePath)
	if err != nil {
		log.Fatalf("initialize database: %v", err)
	}
	defer database.Close()

	accountRepo := repository.NewAccountRepository(database)
	appointmentRepo := repository.NewAppointmentRepository(database)
	googleProvider := provider.NewGoogleProvider(
		os.Getenv("GOOGLE_CLIENT_ID"),
		os.Getenv("GOOGLE_CLIENT_SECRET"),
		getenv("GOOGLE_REDIRECT_URL", "http://localhost:8080/auth/google/callback"),
	)

	appointmentService := service.NewAppointmentService(accountRepo, appointmentRepo, map[string]provider.CalendarProvider{
		model.ProviderGoogle: googleProvider,
	})

	app := fiber.New()
	handler.RegisterHealthRoutes(app)
	handler.NewAuthHandler(accountRepo, googleProvider).Register(app)
	handler.NewConnectionHandler(accountRepo).Register(app)
	handler.NewAppointmentHandler(appointmentService).Register(app)
	app.Static("/", "./web")
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendFile("./web/index.html")
	})

	addr := getenv("APP_ADDR", ":8080")
	log.Printf("listening on %s", addr)
	log.Fatal(app.Listen(addr))
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			if i == 0 {
				return "/"
			}
			return path[:i]
		}
	}
	return "."
}
