package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox"
	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox/sharing"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/nfnt/resize"
	"github.com/streadway/amqp"
)

var dropboxConfig dropbox.Config
var dbPool *pgxpool.Pool

func init() {

	if err := godotenv.Load(); err != nil {
		panic("Error loading .env file")
	}

	// Set Dropbox token from env
	dropboxConfig = dropbox.Config{
		Token:    os.Getenv("DROPBOX_ACCESS_TOKEN"),
		LogLevel: dropbox.LogInfo,
	}

	// Establish RabbitMQ connection
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
	initDB()
	// Start consuming
	go consumeImages(ch)
}

func initDB() {
	// Replace these values with your Render database connection details
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading .env file: %v\n", err)
	}

	// Retrieve PostgreSQL connection URL
	dsn := os.Getenv("POSTGRES_URL")
	if dsn == "" {
		log.Fatalf("POSTGRES_URL is not set in the environment")
	}

	// Connect to PostgreSQL
	dbPool, err = pgxpool.New(context.Background(), dsn)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v\n", err)
	}

	log.Println("Connected to PostgreSQL database successfully!")
	//initSchema()
}

func consumeImages(ch *amqp.Channel) {
	msgs, err := ch.Consume(
		"image_queue", // queue
		"",            // consumer
		true,          // auto-ack
		false,         // exclusive
		false,         // no-local
		false,         // no-wait
		nil,           // arguments
	)
	if err != nil {
		panic("Failed to register a consumer: " + err.Error())
	}

	for msg := range msgs {
		var message struct {
			ProductID int    `json:"product_id"`
			ImageURL  string `json:"image_url"`
		}

		// Unmarshal the JSON message to get product ID and image URL
		err := json.Unmarshal(msg.Body, &message)
		if err != nil {
			fmt.Println("Failed to unmarshal message:", err)
			continue
		}

		fmt.Printf("Processing product ID: %d with image URL: %s\n", message.ProductID, message.ImageURL)

		// Process the image using the image URL
		err = processImage(message.ImageURL, message.ProductID)
		if err != nil {
			fmt.Println("Failed to process image:", err)
		} else {
			fmt.Println("Image processed and saved successfully:", message.ImageURL)
		}
	}
}

func processImage(imageURL string, productID int) error {
	// Fetch the image from the URL
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(imageURL)
	if err != nil {
		return fmt.Errorf("failed to fetch image: %v", err)
	}
	defer resp.Body.Close()

	// Decode the image (supports various formats like PNG, JPEG, GIF)
	img, _, err := image.Decode(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to decode image: %v", err)
	}

	// Resize the image
	resized := resize.Resize(300, 300, img, resize.Lanczos3)

	// Create a temporary buffer to store the resized image
	var buffer bytes.Buffer
	err = jpeg.Encode(&buffer, resized, &jpeg.Options{Quality: 80})
	if err != nil {
		return fmt.Errorf("failed to encode image: %v", err)
	}

	// Set the path to upload to Dropbox
	path := "/" + sanitizeFileName(extractFileName(imageURL))

	// Set up Dropbox sharing client
	sharingClient := sharing.New(dropboxConfig)

	// First, try to create a shared link
	// First, try to create a shared link
	linkArg := &sharing.CreateSharedLinkWithSettingsArg{
		Path: path,
	}

	var shareableURL string
	linkMetadata, err := sharingClient.CreateSharedLinkWithSettings(linkArg)
	if err != nil {
		if dropboxError, ok := err.(dropbox.APIError); ok {
			// Check if the error message contains "shared_link_already_exists/metadata/"
			if strings.Contains(dropboxError.Error(), "shared_link_already_exists/metadata/") {
				log.Println("Shared link already exists for path:", path)

				// Fetch the existing shared link
				existingLinks, err := sharingClient.ListSharedLinks(&sharing.ListSharedLinksArg{
					Path: path,
				})
				if err != nil || len(existingLinks.Links) == 0 {
					return fmt.Errorf("failed to retrieve existing shared link: %v", err)
				}

				// Extract the URL from the first link metadata
				if fileLinkMetadata, ok := existingLinks.Links[0].(*sharing.FileLinkMetadata); ok {
					shareableURL = fileLinkMetadata.Url
					log.Println("Existing shared link for path", path, ": ", shareableURL) // Print path and URL
				} else {
					return fmt.Errorf("unexpected metadata type for shared link")
				}
			} else {
				// Handle other types of errors that might occur
				return fmt.Errorf("failed to create shared link: %v", err)
			}
		} else {
			// If it's not an APIError, log the error and proceed
			log.Println("Error from Dropbox:", err)
			var a string
			a = err.Error()
			if strings.Contains(a, "shared_link_already_exists/metadata/") {
				existingLinks, err := sharingClient.ListSharedLinks(&sharing.ListSharedLinksArg{
					Path: path,
				})
				if err != nil || len(existingLinks.Links) == 0 {
					return fmt.Errorf("failed to retrieve existing shared link: %v", err)
				}

				// Extract the URL from the first link metadata
				if fileLinkMetadata, ok := existingLinks.Links[0].(*sharing.FileLinkMetadata); ok {
					shareableURL = fileLinkMetadata.Url
					log.Println("Existing shared link for path", path, ": ", shareableURL) // Print path and URL
				} else {
					return fmt.Errorf("unexpected metadata type for shared link")
				}
			}
			log.Println("failed to create shared link: %v", err)
		}

	} else {
		// If we successfully created the link, extract the URL
		if link, ok := linkMetadata.(*sharing.FileLinkMetadata); ok {
			shareableURL = link.Url
		} else {
			return fmt.Errorf("unexpected metadata type")
		}
	}

	// Update the product in the database with the new compressed image URL
	query := `UPDATE products 
        SET compressed_product_images = array_append(coalesce(compressed_product_images, '{}'::text[]), $1)
        WHERE id = $2;`
	_, err = dbPool.Exec(context.Background(), query, shareableURL, productID)
	if err != nil {
		return fmt.Errorf("failed to update compressed image URL in database: %v", err)
	}

	fmt.Printf("Sharable link created/retrieved: %s\n", shareableURL)

	return nil
}

// Helper function to sanitize file names
func sanitizeFileName(fileName string) string {
	// Remove query parameters and sanitize the filename
	parsedURL := regexp.MustCompile(`[^\w\s-]`).ReplaceAllString(fileName, "_") // Replace special chars with _
	parsedURL = regexp.MustCompile(`[\s-]+`).ReplaceAllString(parsedURL, "_")   // Replace spaces with underscores
	parsedURL = strings.Trim(parsedURL, "_")                                    // Trim leading/trailing underscores
	parsedURL = parsedURL + ".jpg"                                              // Ensure it has a valid extension
	return parsedURL
}

// Helper function to extract the file name from the URL
func extractFileName(url string) string {
	tokens := strings.Split(url, "/")
	return tokens[len(tokens)-1]
}

// Main function to start the image processing service
func main() {
	// Print a simple message
	fmt.Println("Starting image processing service...")

	// The service will now listen for image processing tasks from the RabbitMQ queue.
	// init() will take care of starting the RabbitMQ connection and consuming messages.
	select {} // This keeps the main function running indefinitely
}
