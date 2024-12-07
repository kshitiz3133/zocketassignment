package main

import (
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/nfnt/resize"
	"github.com/streadway/amqp"
)

func init() {
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

	// Start consuming
	go consumeImages(ch)
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
		imageURL := string(msg.Body)
		fmt.Println("Processing image:", imageURL)

		err := processImage(imageURL)
		if err != nil {
			fmt.Println("Failed to process image:", err)
		} else {
			fmt.Println("Image processed and saved successfully:", imageURL)
		}
	}
}

func processImage(imageURL string) error {
	// Get the image from the URL
	client := &http.Client{Timeout: 10 * time.Second} // Add timeout for network requests
	resp, err := client.Get(imageURL)
	if err != nil {
		return fmt.Errorf("failed to fetch image: %v", err)
	}
	defer resp.Body.Close()

	// Check if the content type is valid
	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		return fmt.Errorf("invalid image format: %v", contentType)
	}

	// Decode the image using image.Decode (supports JPEG, PNG, GIF, BMP, etc.)
	img, format, err := image.Decode(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to decode image: %v", err)
	}
	fmt.Printf("Decoded image format: %s\n", format)

	// Resize the image to 300x300 pixels
	resized := resize.Resize(300, 300, img, resize.Lanczos3)

	// Extract and sanitize the file name from the URL
	fileName := sanitizeFileName(extractFileName(imageURL))
	os.MkdirAll("compressed_images", os.ModePerm)

	// Save the resized image locally as JPEG
	out, err := os.Create("compressed_images/" + fileName)
	if err != nil {
		return fmt.Errorf("failed to create file: %v", err)
	}
	defer out.Close()

	// Compress and save as JPEG
	err = jpeg.Encode(out, resized, &jpeg.Options{Quality: 80})
	if err != nil {
		return fmt.Errorf("failed to compress image: %v", err)
	}

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
