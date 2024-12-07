package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"context"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/streadway/amqp"
)

var dbPool *pgxpool.Pool

func initDB() {
	// Replace these values with your Render database connection details
	dsn := "postgresql://root:P6n7WFCZUvANvYNgaTxOR3bWDJOheCU4@dpg-cta3oht6l47c73bhfv5g-a.oregon-postgres.render.com/zocketdb"

	var err error
	dbPool, err = pgxpool.New(context.Background(), dsn)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v\n", err)
	}

	log.Println("Connected to PostgreSQL database successfully!")
	//initSchema()
}

// func initSchema() {
// 	query := `
//     CREATE TABLE IF NOT EXISTS products (
//         id SERIAL PRIMARY KEY,
//         user_id INT NOT NULL,
//         product_name TEXT NOT NULL,
//         product_description TEXT,
//         product_images TEXT[], -- Array of image URLs
//         product_price NUMERIC(10, 2) NOT NULL
//     );`

// 	_, err := dbPool.Exec(context.Background(), query)
// 	if err != nil {
// 		log.Fatalf("Failed to create table: %v\n", err)
// 	}
// 	log.Println("Database schema initialized")
// }

// Product struct to define product fields
type Product struct {
	ID                 int      `json:"id"`
	UserID             int      `json:"user_id"`
	ProductName        string   `json:"product_name"`
	ProductDescription string   `json:"product_description"`
	ProductImages      []string `json:"product_images"`
	ProductPrice       float64  `json:"product_price"`
}

// Response structure to send JSON data
type Response struct {
	Message string `json:"message"`
}

// In-memory product storage
var products = []Product{}
var nextID = 1
var amqpChannel *amqp.Channel

func init() {
	// Establish connection with RabbitMQ
	conn, err := amqp.Dial("amqp://guest:guest@localhost:5672/")
	if err != nil {
		panic("Failed to connect to RabbitMQ: " + err.Error())
	}

	ch, err := conn.Channel()
	if err != nil {
		panic("Failed to open a channel: " + err.Error())
	}

	// Declare a queue
	_, err = ch.QueueDeclare(
		"image_queue", // name
		true,          // durable
		false,         // delete when unused
		false,         // exclusive
		false,         // no-wait
		nil,           // arguments
	)
	if err != nil {
		panic("Failed to declare a queue: " + err.Error())
	}
	log.Println("Connected to Consumer successfully!")
	amqpChannel = ch
}

func main() {
	initDB() // Initialize the database connection
	defer dbPool.Close()
	err := dbPool.Ping(context.Background())
	if err != nil {
		log.Fatalf("Unable to ping database: %v\n", err)
	}

	fmt.Println("Database connection is alive!")
	// Define the /products handler
	http.HandleFunc("/products", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			createProductHandler(w, r)
		} else if r.Method == http.MethodGet {
			getProductsHandler(w, r)
		} else {
			http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		}
	})

	// Define the /products/:id handler
	http.HandleFunc("/products/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			getProductByIDHandler(w, r)
		} else {
			http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		}
	})

	http.HandleFunc("/cleanall", cleanAllHandler)

	// Start the HTTP server
	const addr = ":8080"
	println("Server is running on http://localhost" + addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		println("Error starting server:", err.Error())
	}
}

// Handler to create a new product
func createProductHandler(w http.ResponseWriter, r *http.Request) {
	var product Product
	err := json.NewDecoder(r.Body).Decode(&product)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Insert product into the database
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

	// Publish each image URL to RabbitMQ
	for _, imageURL := range product.ProductImages {
		err := amqpChannel.Publish(
			"",            // exchange
			"image_queue", // routing key
			false,         // mandatory
			false,         // immediate
			amqp.Publishing{
				ContentType: "text/plain",
				Body:        []byte(imageURL),
			},
		)
		if err != nil {
			http.Error(w, "Failed to enqueue image URL", http.StatusInternalServerError)
			return
		}
		log.Printf("Enqueued image URL: %s", imageURL) // Log the URL that's enqueued
	}

	// Respond with the created product
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(product)
}

// Handler to get a product by ID
func getProductByIDHandler(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/products/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid product ID", http.StatusBadRequest)
		return
	}

	query := `SELECT id, user_id, product_name, product_description, product_images, product_price FROM products WHERE id = $1;`
	var product Product
	err = dbPool.QueryRow(context.Background(), query, id).Scan(&product.ID, &product.UserID, &product.ProductName, &product.ProductDescription, &product.ProductImages, &product.ProductPrice)
	if err != nil {
		http.Error(w, "Product not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(product)
}

// Handler to get all products
func getProductsHandler(w http.ResponseWriter, _ *http.Request) {
	query := `SELECT id, user_id, product_name, product_description, product_images, product_price FROM products;`

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
		err := rows.Scan(&product.ID, &product.UserID, &product.ProductName, &product.ProductDescription, &product.ProductImages, &product.ProductPrice)
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
	// Only allow DELETE method for this route
	if r.Method != http.MethodDelete {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// Perform the deletion query
	query := `DELETE FROM products;`
	_, err := dbPool.Exec(context.Background(), query)
	if err != nil {
		http.Error(w, "Failed to delete all products", http.StatusInternalServerError)
		log.Printf("Error deleting products: %v\n", err)
		return
	}

	// Send a success response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(Response{Message: "All products deleted successfully"})
}
