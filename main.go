package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/streadway/amqp"
)

var dbPool *pgxpool.Pool

func initDB() {
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading .env file: %v\n", err)
	}

	dsn := os.Getenv("POSTGRES_URL")
	if dsn == "" {
		log.Fatalf("POSTGRES_URL is not set in the environment")
	}

	dbPool, err = pgxpool.New(context.Background(), dsn)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v\n", err)
	}

	log.Println("Connected to PostgreSQL database successfully!")
	//initSchema()
}

// func initSchema() {
// 	query := `
//     ALTER TABLE products
// 	ADD COLUMN compressed_product_images TEXT[];
// 	`

// 	_, err := dbPool.Exec(context.Background(), query)
// 	if err != nil {
// 		log.Fatalf("Failed to create table: %v\n", err)
// 	}
// 	log.Println("Database schema initialized")
// }

type Product struct {
	ID                      int      `json:"id"`
	UserID                  int      `json:"user_id"`
	ProductName             string   `json:"product_name"`
	ProductDescription      string   `json:"product_description"`
	ProductImages           []string `json:"product_images"`
	CompressedProductImages []string `json:"compressed_product_images"`
	ProductPrice            float64  `json:"product_price"`
}

type Response struct {
	Message string `json:"message"`
}

var products = []Product{}
var nextID = 1
var amqpChannel *amqp.Channel

func init() {
	conn, err := amqp.Dial("amqp://guest:guest@localhost:5672/")
	if err != nil {
		panic("Failed to connect to RabbitMQ: " + err.Error())
	}

	ch, err := conn.Channel()
	if err != nil {
		panic("Failed to open a channel: " + err.Error())
	}

	_, err = ch.QueueDeclare(
		"image_queue",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		panic("Failed to declare a queue: " + err.Error())
	}
	log.Println("Connected to Consumer successfully!")
	amqpChannel = ch
}

func main() {
	initDB()
	defer dbPool.Close()
	err := dbPool.Ping(context.Background())
	if err != nil {
		log.Fatalf("Unable to ping database: %v\n", err)
	}

	fmt.Println("Database connection is alive!")
	http.HandleFunc("/products", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			createProductHandler(w, r)
		} else if r.Method == http.MethodGet {
			getProductsHandler(w, r)
		} else {
			http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		}
	})

	http.HandleFunc("/products/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			getProductByIDHandler(w, r)
		} else {
			http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		}
	})

	http.HandleFunc("/cleanall", cleanAllHandler)

	const addr = ":8080"
	println("Server is running on http://localhost" + addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		println("Error starting server:", err.Error())
	}
}

func createProductHandler(w http.ResponseWriter, r *http.Request) {
	var product Product
	err := json.NewDecoder(r.Body).Decode(&product)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	query := `
    INSERT INTO products (user_id, product_name, product_description, product_images, product_price)
    VALUES ($1, $2, $3, $4, $5)
    RETURNING id;
    `
	err = dbPool.QueryRow(context.Background(), query, product.UserID, product.ProductName, product.ProductDescription, product.ProductImages, product.ProductPrice).Scan(&product.ID)
	if err != nil {
		http.Error(w, "Failed to create product", http.StatusInternalServerError)
		log.Printf("Error inserting product: %v\n", err)
		return
	}

	log.Printf("Enqueued product ID %d with image URLs", product.ID)
	for _, imageURL := range product.ProductImages {
		message := map[string]interface{}{
			"product_id": product.ID,
			"image_url":  imageURL,
		}

		messageJSON, err := json.Marshal(message)
		if err != nil {
			http.Error(w, "Failed to create JSON message", http.StatusInternalServerError)
			return
		}

		err = amqpChannel.Publish(
			"",
			"image_queue",
			false,
			false,
			amqp.Publishing{
				ContentType: "application/json",
				Body:        messageJSON,
			},
		)
		if err != nil {
			http.Error(w, "Failed to enqueue image URL", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(product)
}

func getProductByIDHandler(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/products/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid product ID", http.StatusBadRequest)
		return
	}

	query := `SELECT id, user_id, product_name, product_description, product_images, compressed_product_images, product_price FROM products WHERE id = $1;`
	var product Product
	err = dbPool.QueryRow(context.Background(), query, id).Scan(&product.ID, &product.UserID, &product.ProductName, &product.ProductDescription, &product.ProductImages, &product.CompressedProductImages, &product.ProductPrice)
	if err != nil {
		http.Error(w, "Product not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(product)
}

func getProductsHandler(w http.ResponseWriter, _ *http.Request) {
	query := `SELECT id, user_id, product_name, product_description, product_images, compressed_product_images, product_price FROM products;`

	rows, err := dbPool.Query(context.Background(), query)
	if err != nil {
		http.Error(w, "Failed to fetch products", http.StatusInternalServerError)
		log.Printf("Error fetching products: %v\n", err)
		return
	}
	defer rows.Close()

	var products []Product
	for rows.Next() {
		var product Product

		err := rows.Scan(&product.ID, &product.UserID, &product.ProductName, &product.ProductDescription, &product.ProductImages, &product.CompressedProductImages, &product.ProductPrice)
		if err != nil {
			http.Error(w, "Failed to parse products", http.StatusInternalServerError)
			log.Printf("Error parsing product: %v\n", err)
			return
		}

		products = append(products, product)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(products)
}

func cleanAllHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	query := `DELETE FROM products;`
	_, err := dbPool.Exec(context.Background(), query)
	if err != nil {
		http.Error(w, "Failed to delete all products", http.StatusInternalServerError)
		log.Printf("Error deleting products: %v\n", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(Response{Message: "All products deleted successfully"})
}
