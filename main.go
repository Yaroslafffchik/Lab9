package main

import (
	"database/sql"
	"fmt"
	"github.com/gofiber/adaptor/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/swagger"
	"github.com/gofiber/websocket/v2"
	"github.com/graphql-go/graphql"
	"github.com/graphql-go/handler"
	"github.com/joho/godotenv"
	"github.com/lib/pq"
	"log"
	"os"
	_ "server/docs"
	"time"
)

type ErrorResponse struct {
	Error string `json:"error"`
}

var db *sql.DB

func initDB() {
	err := godotenv.Load()
	if err != nil {
		log.Println("Не найден файл .env, используются значения по умолчанию")
	}

	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		os.Getenv("DB_HOST"), os.Getenv("DB_PORT"), os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"))

	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(time.Hour)

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS products (
			id SERIAL PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			price DECIMAL(10, 2) NOT NULL,
			description TEXT,
			categories TEXT[]
		);
	`)
	if err != nil {
		log.Fatal(err)
	}
}

type Product struct {
	ID          int      `json:"id"`
	Name        string   `json:"name"`
	Price       float64  `json:"price"`
	Description string   `json:"description"`
	Categories  []string `json:"categories"`
}

// @Summary Получение списка всех продуктов
// @Tags Products
// @Accept json
// @Produce json
// @Success 200 {array} Product "Успешный ответ"
// @Failure 500 {object} ErrorResponse "Ошибка на сервере"
// @Router /api/products [get]
func getProducts(c *fiber.Ctx) error {
	rows, err := db.Query("SELECT id, name, price, description, categories FROM products")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(ErrorResponse{Error: err.Error()})
	}
	defer rows.Close()

	var products []Product
	for rows.Next() {
		var product Product
		if err := rows.Scan(&product.ID, &product.Name, &product.Price, &product.Description, pq.Array(&product.Categories)); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(ErrorResponse{Error: err.Error()})
		}
		products = append(products, product)
	}

	if err := rows.Err(); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(ErrorResponse{Error: err.Error()})
	}

	return c.JSON(products)
}

// @Summary Добавить один или несколько продуктов
// @Tags Products
// @Accept json
// @Produce json
// @Param products body []Product true "Данные продуктов"
// @Success 200 {array} Product "Продукты успешно добавлены"
// @Failure 400 {object} ErrorResponse "Некорректный запрос"
// @Failure 500 {object} ErrorResponse "Ошибка на сервере"
// @Router /api/products [post]
func addProducts(c *fiber.Ctx) error {
	var products []Product

	if err := c.BodyParser(&products); err != nil {
		var singleProduct Product
		if err := c.BodyParser(&singleProduct); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(ErrorResponse{Error: "Invalid request"})
		}
		products = append(products, singleProduct)
	}

	query := "INSERT INTO products (name, price, description, categories) VALUES ($1, $2, $3, $4) RETURNING id"

	for i := range products {
		err := db.QueryRow(query, products[i].Name, products[i].Price, products[i].Description, pq.Array(products[i].Categories)).Scan(&products[i].ID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(ErrorResponse{Error: err.Error()})
		}
	}

	return c.JSON(products)
}

// @Summary Обновить данные продукта
// @Tags Products
// @Accept json
// @Produce json
// @Param id path int true "ID продукта"
// @Param product body Product true "Данные продукта"
// @Success 200 {object} map[string]string "Продукт успешно обновлен"
// @Failure 400 {object} ErrorResponse "Некорректный запрос"
// @Failure 500 {object} ErrorResponse "Ошибка на сервере"
// @Router /api/products/{id} [put]
func updateProduct(c *fiber.Ctx) error {
	id := c.Params("id")
	var product Product
	if err := c.BodyParser(&product); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(ErrorResponse{Error: "Invalid request"})
	}

	query := "UPDATE products SET name=$1, price=$2, description=$3, categories=$4 WHERE id=$5"
	_, err := db.Exec(query, product.Name, product.Price, product.Description, pq.Array(product.Categories), id)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(ErrorResponse{Error: err.Error()})
	}
	return c.JSON(fiber.Map{"message": "Product updated successfully"})
}

// @Summary Удалить продукт
// @Tags Products
// @Accept json
// @Produce json
// @Param id path int true "ID продукта"
// @Success 200 {object} map[string]string "Продукт успешно удален"
// @Failure 500 {object} ErrorResponse "Ошибка на сервере"
// @Router /api/products/{id} [delete]
func deleteProduct(c *fiber.Ctx) error {
	id := c.Params("id")
	query := "DELETE FROM products WHERE id=$1"
	_, err := db.Exec(query, id)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(ErrorResponse{Error: err.Error()})
	}
	return c.JSON(fiber.Map{"message": "Product deleted successfully"})
}

var productType = graphql.NewObject(
	graphql.ObjectConfig{
		Name: "Product",
		Fields: graphql.Fields{
			"id":          &graphql.Field{Type: graphql.Int},
			"name":        &graphql.Field{Type: graphql.String},
			"price":       &graphql.Field{Type: graphql.Float},
			"description": &graphql.Field{Type: graphql.String},
			"categories":  &graphql.Field{Type: graphql.NewList(graphql.String)},
		},
	},
)

func createSchema() graphql.Schema {
	rootQuery := graphql.NewObject(graphql.ObjectConfig{
		Name: "Query",
		Fields: graphql.Fields{
			"products": &graphql.Field{
				Type: graphql.NewList(productType),
				Resolve: func(params graphql.ResolveParams) (interface{}, error) {
					rows, err := db.Query("SELECT id, name, price, description, categories FROM products")
					if err != nil {
						return nil, err
					}
					defer rows.Close()

					var products []Product
					for rows.Next() {
						var product Product
						if err := rows.Scan(&product.ID, &product.Name, &product.Price, &product.Description, pq.Array(&product.Categories)); err != nil {
							return nil, err
						}
						products = append(products, product)
					}
					return products, nil
				},
			},
		},
	})

	schema, err := graphql.NewSchema(graphql.SchemaConfig{
		Query: rootQuery,
	})
	if err != nil {
		log.Fatalf("Не удалось создать схему GraphQL: %v", err)
	}
	return schema
}

type Message struct {
	Username string `json:"username"`
	Message  string `json:"message"`
}

var clients = make(map[*websocket.Conn]bool)
var broadcast = make(chan Message)

func handleMessages() {
	for {
		msg := <-broadcast
		for client := range clients {
			if err := client.WriteJSON(msg); err != nil {
				log.Printf("Ошибка отправки сообщения WebSocket: %v", err)
				client.Close()
				delete(clients, client)
			}
		}
	}
}

// @title TEST API
// @version 1.0
// @BasePath /
func main() {
	initDB()
	defer db.Close()

	app := fiber.New()

	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowMethods: "GET,POST,PUT,DELETE",
		AllowHeaders: "Origin, Content-Type, Accept",
	}))

	app.Static("/", "./public")

	app.Get("/api/products", getProducts)
	app.Post("/api/products", addProducts)
	app.Put("/api/products/:id", updateProduct)
	app.Delete("/api/products/:id", deleteProduct)
	app.Get("/health", func(c *fiber.Ctx) error { return c.SendString("hello") })

	schema := createSchema()
	graphqlHandler := handler.New(&handler.Config{
		Schema: &schema,
		Pretty: true,
	})
	app.All("/api/graphql", adaptor.HTTPHandler(graphqlHandler))

	go handleMessages()

	app.Get("/api/ws", websocket.New(func(c *websocket.Conn) {
		clients[c] = true
		defer func() {
			delete(clients, c)
			c.Close()
		}()
		for {
			var msg Message
			if err := c.ReadJSON(&msg); err != nil {
				log.Printf("Ошибка WebSocket: %v", err)
				break
			}
			broadcast <- msg
		}
	}))

	app.Get("/swagger/*", swagger.HandlerDefault)

	log.Println("Сервер запущен на порту 8080")
	log.Fatal(app.Listen(":8080"))
}
